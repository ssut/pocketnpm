package npm

type MirrorConfig struct {
	Registry       string `toml:"registry"`
	MaxConnections int    `toml:"max_connections"`
	Path           string `toml:"path"`
}

type AllDocsResponse struct {
	TotalRows int       `json:"total_rows"`
	Offset    int       `json:"offset"`
	Sequence  int       `json:"update_seq"`
	Rows      []docsRow `json:"rows"`
}

type docsRow struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Value struct {
		Revision string `json:"rev"`
	} `json:"value"`
}

type ChangesResponse struct {
	Results      []changesResult `json:"results"`
	LastSequence int             `json:"last_seq"`
}

type changesResult struct {
	Sequence int    `json:"seq"`
	ID       string `json:"id"`
	Changes  struct {
		Revision string `json:"rev"`
	}
}
