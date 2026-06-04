package webresearch

import "github.com/Suren878/matrixclaw/internal/work"

type ArtifactStore = work.ArtifactStore

func NewArtifactStore(root string) *ArtifactStore {
	return work.NewArtifactStore(root)
}
