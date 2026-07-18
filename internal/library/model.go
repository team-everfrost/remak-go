package library

import "time"

type Document struct {
	DocID        string    `json:"docId"`
	Title        string    `json:"title"`
	Type         string    `json:"type"`
	URL          string    `json:"url"`
	Content      string    `json:"content"`
	Summary      string    `json:"summary"`
	Status       string    `json:"status"`
	ThumbnailURL string    `json:"thumbnailUrl"`
	FileSize     int64     `json:"fileSize,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Tags         []string  `json:"tags"`
}

type MemoInput struct {
	Content string `json:"content"`
}

type WebpageInput struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content,omitempty"`
}

type Cursor struct {
	Time  *time.Time
	DocID string
	Limit int32
}

type Tag struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type Collection struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Count       int64  `json:"count"`
}

type CreateCollectionInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DocIDs      []string `json:"docIds,omitempty"`
}

type UpdateCollectionInput struct {
	NewName       *string  `json:"newName,omitempty"`
	Description   *string  `json:"description,omitempty"`
	AddedDocIDs   []string `json:"addedDocIds,omitempty"`
	RemovedDocIDs []string `json:"removedDocIds,omitempty"`
}

type AddDocumentsInput struct {
	DocIDs []string `json:"docIds"`
}
