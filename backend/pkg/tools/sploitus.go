package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pentagi/pkg/database"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/observability/langfuse"

	"github.com/sirupsen/logrus"
)

const (
	sploitusAPIURL        = "https://sploitus.com/search"
	sploitusDefaultSort   = "default"
	defaultSploitusLimit  = 10
	maxSploitusLimit      = 25
	defaultSploitusType   = "exploits"
	sploitusRequestTimeout = 30 * time.Second
)

// sploitus represents the Sploitus exploit search tool
type sploitus struct {
	flowID    int64
	taskID    *int64
	subtaskID *int64
	enabled   bool
	proxyURL  string
	slp       SearchLogProvider
}

// NewSploitusTool creates a new Sploitus search tool instance
func NewSploitusTool(
	flowID int64,
	taskID, subtaskID *int64,
	enabled bool,
	proxyURL string,
	slp SearchLogProvider,
) Tool {
	return &sploitus{
		flowID:    flowID,
		taskID:    taskID,
		subtaskID: subtaskID,
		enabled:   enabled,
		proxyURL:  proxyURL,
		slp:       slp,
	}
}

// IsAvailable returns true if the Sploitus tool is enabled and configured
func (s *sploitus) IsAvailable() bool {
	return s.enabled && s.slp != nil
}

// Handle processes a Sploitus exploit search request from an AI agent
func (s *sploitus) Handle(ctx context.Context, name string, args json.RawMessage) (string, error) {
	var action SploitusAction
	ctx, observation := obs.Observer.NewObservation(ctx)
	logger := logrus.WithContext(ctx).WithFields(logrus.Fields{
		"tool": name,
		"args": string(args),
	})

	if err := json.Unmarshal(args, &action); err != nil {
		logger.WithError(err).Error("failed to unmarshal sploitus search action")
		return "", fmt.Errorf("failed to unmarshal %s search action arguments: %w", name, err)
	}

	// Normalise exploit type
	exploitType := strings.ToLower(strings.TrimSpace(action.ExploitType))
	if exploitType == "" {
		exploitType = defaultSploitusType
	}

	// Normalise sort order
	sort := strings.ToLower(strings.TrimSpace(action.Sort))
	if sort == "" {
		sort = sploitusDefaultSort
	}

	// Clamp max results
	limit := action.MaxResults.Int()
	if limit < 1 || limit > maxSploitusLimit {
		limit = defaultSploitusLimit
	}

	logger = logger.WithFields(logrus.Fields{
		"query":        action.Query[:min(len(action.Query), 1000)],
		"exploit_type": exploitType,
		"sort":         sort,
		"limit":        limit,
	})

	result, err := s.search(ctx, action.Query, exploitType, sort, limit)
	if err != nil {
		observation.Event(
			langfuse.WithEventName("sploitus search error swallowed"),
			langfuse.WithEventInput(action.Query),
			langfuse.WithEventStatus(err.Error()),
			langfuse.WithEventLevel(langfuse.ObservationLevelWarning),
			langfuse.WithEventMetadata(langfuse.Metadata{
				"tool_name":    SploitusToolName,
				"engine":       "sploitus",
				"query":        action.Query,
				"exploit_type": exploitType,
				"sort":         sort,
				"limit":        limit,
				"error":        err.Error(),
			}),
		)

		logger.WithError(err).Error("failed to search in Sploitus")
		return fmt.Sprintf("failed to search in Sploitus: %v", err), nil
	}

	if agentCtx, ok := GetAgentContext(ctx); ok {
		_, _ = s.slp.PutLog(
			ctx,
			agentCtx.ParentAgentType,
			agentCtx.CurrentAgentType,
			database.SearchengineTypeSploitus,
			action.Query,
			result,
			s.taskID,
			s.subtaskID,
		)
	}

	return result, nil
}

// sploitusRequest is the JSON body sent to the Sploitus search API
type sploitusRequest struct {
	Query  string `json:"query"`
	Type   string `json:"type"`
	Sort   string `json:"sort"`
	Offset int    `json:"offset"`
}

// sploitusCVSS holds CVSS scoring information for an exploit
type sploitusCVSS struct {
	Score  float64 `json:"score"`
	Vector string  `json:"vector"`
}

// sploitusExploit represents a single exploit record returned by Sploitus
type sploitusExploit struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	Type       string       `json:"type"`
	Source     string       `json:"source"`
	URL        string       `json:"url"`
	Published  string       `json:"published"`
	Hash       string       `json:"hash"`
	VHash      string       `json:"vhash"`
	CVSS       sploitusCVSS `json:"cvss"`
	References []string     `json:"references"`
}

// sploitusTool represents a security tool record returned by Sploitus
type sploitusTool struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Source    string   `json:"source"`
	URL       string   `json:"url"`
	Published string   `json:"published"`
	Hash      string   `json:"hash"`
	Tags      []string `json:"tags"`
}

// sploitusResponse is the top-level JSON response from the Sploitus API
type sploitusResponse struct {
	Exploits []sploitusExploit `json:"exploits"`
	Tools    []sploitusTool    `json:"tools"`
	Total    int               `json:"total"`
}

// search calls the Sploitus API and returns a formatted markdown result string
func (s *sploitus) search(ctx context.Context, query, exploitType, sort string, limit int) (string, error) {
	reqBody := sploitusRequest{
		Query:  query,
		Type:   exploitType,
		Sort:   sort,
		Offset: 0,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Build HTTP client, optionally routed through a proxy
	httpClient := &http.Client{Timeout: sploitusRequestTimeout}
	if s.proxyURL != "" {
		proxyParsed, parseErr := url.Parse(s.proxyURL)
		if parseErr != nil {
			return "", fmt.Errorf("invalid proxy URL: %w", parseErr)
		}
		httpClient.Transport = &http.Transport{Proxy: http.ProxyURL(proxyParsed)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sploitusAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "PentAGI/1.0 (security research tool)")
	req.Header.Set("Origin", "https://sploitus.com")
	req.Header.Set("Referer", "https://sploitus.com/")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request to Sploitus failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Sploitus API returned HTTP %d", resp.StatusCode)
	}

	var apiResp sploitusResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("failed to decode Sploitus response: %w", err)
	}

	return formatSploitusResults(query, exploitType, limit, apiResp), nil
}

// formatSploitusResults converts a sploitusResponse into a human-readable markdown string
func formatSploitusResults(query, exploitType string, limit int, resp sploitusResponse) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Sploitus Search Results\n\n"))
	sb.WriteString(fmt.Sprintf("**Query:** `%s`  \n", query))
	sb.WriteString(fmt.Sprintf("**Type:** %s  \n", exploitType))
	sb.WriteString(fmt.Sprintf("**Total matches on Sploitus:** %d\n\n", resp.Total))
	sb.WriteString("---\n\n")

	switch strings.ToLower(exploitType) {
	case "tools":
		tools := resp.Tools
		if len(tools) > limit {
			tools = tools[:limit]
		}
		if len(tools) == 0 {
			sb.WriteString("No security tools were found for the given query.\n")
			return sb.String()
		}

		sb.WriteString(fmt.Sprintf("## Security Tools (%d shown)\n\n", len(tools)))
		for i, t := range tools {
			sb.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, t.Title))
			if t.URL != "" {
				sb.WriteString(fmt.Sprintf("**URL:** %s  \n", t.URL))
			}
			if t.Source != "" {
				sb.WriteString(fmt.Sprintf("**Source:** %s  \n", t.Source))
			}
			if t.Published != "" {
				sb.WriteString(fmt.Sprintf("**Published:** %s  \n", t.Published))
			}
			if len(t.Tags) > 0 {
				sb.WriteString(fmt.Sprintf("**Tags:** %s  \n", strings.Join(t.Tags, ", ")))
			}
			sb.WriteString("\n---\n\n")
		}

	default: // "exploits" or anything else
		exploits := resp.Exploits
		if len(exploits) > limit {
			exploits = exploits[:limit]
		}
		if len(exploits) == 0 {
			sb.WriteString("No exploits were found for the given query.\n")
			return sb.String()
		}

		sb.WriteString(fmt.Sprintf("## Exploits (%d shown)\n\n", len(exploits)))
		for i, e := range exploits {
			sb.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, e.Title))
			if e.URL != "" {
				sb.WriteString(fmt.Sprintf("**URL:** %s  \n", e.URL))
			}
			if e.Source != "" {
				sb.WriteString(fmt.Sprintf("**Source:** %s  \n", e.Source))
			}
			if e.Published != "" {
				sb.WriteString(fmt.Sprintf("**Published:** %s  \n", e.Published))
			}
			if e.Type != "" {
				sb.WriteString(fmt.Sprintf("**Type:** %s  \n", e.Type))
			}
			if e.CVSS.Score > 0 {
				sb.WriteString(fmt.Sprintf("**CVSS Score:** %.1f  \n", e.CVSS.Score))
				if e.CVSS.Vector != "" {
					sb.WriteString(fmt.Sprintf("**CVSS Vector:** `%s`  \n", e.CVSS.Vector))
				}
			}
			if len(e.References) > 0 {
				sb.WriteString(fmt.Sprintf("**CVE References:** %s  \n", strings.Join(e.References, ", ")))
			}
			sb.WriteString("\n---\n\n")
		}
	}

	return sb.String()
}
