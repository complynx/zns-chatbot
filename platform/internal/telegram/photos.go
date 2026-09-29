package telegram

// PhotoSize identifies one Telegram-generated variant, not the camera original.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	UniqueID string `json:"file_unique_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Size     int64  `json:"file_size,omitempty"`
}

// PhotoDocument selects the largest bounded variant independently of array order.
// Download still checks the actual body when Telegram omits the declared size.
func PhotoDocument(sizes []PhotoSize) (Document, error) {
	const maxDimension = 20000
	var selected PhotoSize
	var largest int64
	for _, size := range sizes {
		if size.FileID == "" || size.Width <= 0 || size.Height <= 0 ||
			size.Width > maxDimension || size.Height > maxDimension ||
			size.Size < 0 || size.Size > MaxDocumentBytes {
			continue
		}
		area := int64(size.Width) * int64(size.Height)
		if area > largest {
			selected, largest = size, area
		}
	}
	if largest == 0 {
		return Document{}, ErrInvalidDocument
	}
	return Document{FileID: selected.FileID, UniqueID: selected.UniqueID,
		Filename: "photo.jpg", MIME: "image/jpeg", Size: selected.Size}, nil
}
