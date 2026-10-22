package authsvc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"lifelog-api/internal/httpclient"
)

type HTTPMergeNotifier struct {
	targets []string

	subscriptionTargets []string
	token               string
	client              *http.Client
}

func NewHTTPMergeNotifier(targets, subscriptionTargets []string, token string, timeout time.Duration) *HTTPMergeNotifier {
	return &HTTPMergeNotifier{
		targets:             cleanTargets(targets),
		subscriptionTargets: cleanTargets(subscriptionTargets),
		token:               token,
		client:              httpclient.Internal(timeout),
	}
}

func cleanTargets(targets []string) []string {
	cleaned := make([]string, 0, len(targets))
	for _, target := range targets {
		if trimmed := strings.TrimSpace(target); trimmed != "" {
			cleaned = append(cleaned, strings.TrimRight(trimmed, "/"))
		}
	}
	return cleaned
}

func (n *HTTPMergeNotifier) NotifyMerge(ctx context.Context, from, into uuid.UUID) error {
	payload, err := json.Marshal(map[string]string{"from": from.String(), "into": into.String()})
	if err != nil {
		return err
	}

	for _, target := range n.targets {
		if err := n.postInternal(ctx, target+"/internal/users/merge", payload); err != nil {
			return err
		}
	}
	return nil
}

func (n *HTTPMergeNotifier) postInternal(ctx context.Context, url string, payload []byte) error {
	return httpclient.Call(ctx, n.client, http.MethodPost, url, n.token, json.RawMessage(payload), nil)
}
