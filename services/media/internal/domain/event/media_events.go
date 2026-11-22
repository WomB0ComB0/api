package event

const (
	EventAssetUploaded = "media.asset.uploaded"
	EventAssetDeleted  = "media.asset.deleted"
)

type AssetUploadedEvent struct {
	BaseEvent
	UserID     string
	R2Key      string
	PublicURL  string
	Visibility string
	SizeBytes  int64
}

func NewAssetUploadedEvent(assetID, userID, r2Key, publicURL, visibility string, sizeBytes int64) AssetUploadedEvent {
	return AssetUploadedEvent{
		BaseEvent:  NewBaseEvent(EventAssetUploaded, assetID),
		UserID:     userID,
		R2Key:      r2Key,
		PublicURL:  publicURL,
		Visibility: visibility,
		SizeBytes:  sizeBytes,
	}
}

type AssetDeletedEvent struct {
	BaseEvent
	UserID     string
	R2Key      string
	PublicURL  string
	Visibility string
}

func NewAssetDeletedEvent(assetID, userID, r2Key, publicURL, visibility string) AssetDeletedEvent {
	return AssetDeletedEvent{
		BaseEvent:  NewBaseEvent(EventAssetDeleted, assetID),
		UserID:     userID,
		R2Key:      r2Key,
		PublicURL:  publicURL,
		Visibility: visibility,
	}
}
