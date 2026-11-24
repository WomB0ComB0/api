package application

import (
	"api/media/internal/application/media"
	"api/media/internal/domain/event"
	"api/media/internal/domain/port"
)

// Factory wires and exposes all application use cases (inspired by go-hexagonal/application/factory.go).
type Factory struct {
	store          port.ObjectStore
	repo           port.AssetRepository
	signer         port.URLSigner
	purger         port.CachePurger
	bus            event.EventBus
	cdnBaseURL     string
	maxUploadBytes int64
}

func NewFactory(
	store port.ObjectStore,
	repo port.AssetRepository,
	signer port.URLSigner,
	purger port.CachePurger,
	bus event.EventBus,
	cdnBaseURL string,
	maxUploadBytes int64,
) *Factory {
	if bus == nil {
		bus = event.NoopEventBus{}
	}
	return &Factory{
		store:          store,
		repo:           repo,
		signer:         signer,
		purger:         purger,
		bus:            bus,
		cdnBaseURL:     cdnBaseURL,
		maxUploadBytes: maxUploadBytes,
	}
}

func (f *Factory) Upload() *media.UploadUseCase {
	return media.NewUploadUseCase(f.store, f.repo, f.signer, f.bus, f.cdnBaseURL, f.maxUploadBytes)
}

func (f *Factory) PresignUpload() *media.PresignUploadUseCase {
	return media.NewPresignUploadUseCase(f.store, f.cdnBaseURL)
}

func (f *Factory) ConfirmUpload() *media.ConfirmUploadUseCase {
	return media.NewConfirmUploadUseCase(f.store, f.repo, f.signer, f.bus, f.cdnBaseURL, f.maxUploadBytes)
}

func (f *Factory) GetAsset() *media.GetAssetUseCase {
	return media.NewGetAssetUseCase(f.repo, f.signer, f.cdnBaseURL)
}

func (f *Factory) ListAssets() *media.ListAssetsUseCase {
	return media.NewListAssetsUseCase(f.store, f.repo, f.signer, f.cdnBaseURL)
}

func (f *Factory) DeleteAsset() *media.DeleteAssetUseCase {
	return media.NewDeleteAssetUseCase(f.store, f.repo, f.bus)
}

func (f *Factory) SignURL() *media.SignURLUseCase {
	return media.NewSignURLUseCase(f.repo, f.signer, f.cdnBaseURL)
}

func (f *Factory) PurgeCache() *media.PurgeCacheUseCase {
	return media.NewPurgeCacheUseCase(f.purger, f.cdnBaseURL)
}

func (f *Factory) ObjectStore() port.ObjectStore        { return f.store }
func (f *Factory) AssetRepository() port.AssetRepository { return f.repo }
