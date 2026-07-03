package provider

import (
	"context"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/oci"
)

// DiscoverOCITags lists an OCI repository's tags as candidates via the shared
// registry client, honoring a deep lookup and noting truncation when a shallow
// lookup left a further page unread so the edge can suggest --deep. Registry
// tag lists carry no timestamps, so cooldown does not apply to these
// candidates.
func DiscoverOCITags(
	ctx context.Context,
	client *oci.Client,
	repo oci.Repo,
	describe, url string,
) ([]model.Candidate, error) {
	tags, truncated, err := client.Tags(ctx, repo, Deep(ctx))
	if err != nil {
		return nil, err
	}
	if truncated {
		NoteTruncated(ctx, describe, url)
	}
	candidates := make([]model.Candidate, 0, len(tags))
	for _, t := range tags {
		candidates = append(candidates, model.NewVariantCandidate(t))
	}
	return candidates, nil
}
