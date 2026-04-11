package main

type subsonicResponse struct {
	SubsonicResponse subsonicPayload `json:"subsonic-response"`
}

type subsonicPayload struct {
	Status  string         `json:"status,omitempty"`
	Artists *artistsIndex  `json:"artists,omitempty"`
	Artist  *artistPayload `json:"artist,omitempty"`
	Album   *albumPayload  `json:"album,omitempty"`
	Song    *rawSong       `json:"song,omitempty"`
}

type artistsIndex struct {
	Index []indexEntry `json:"index"`
}

type indexEntry struct {
	Artist []rawArtist `json:"artist"`
}

type rawArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type artistPayload struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Album []rawAlbum `json:"album"`
}

type rawAlbum struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ArtistId string `json:"artistId,omitempty"`
	Created  string `json:"created,omitempty"`
}

type albumPayload struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Song []rawSong `json:"song"`
}

type rawSong struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	Created     string `json:"created,omitempty"`
}
