package memory

// SQLite syncs its own database and WAL. Go cannot portably fsync a Windows directory.
func syncDirectory(string) error { return nil }
