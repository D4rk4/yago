package searchindex

import "fmt"

func (b *BleveMemoryIndex) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if err := b.index.Close(); err != nil {
		return fmt.Errorf("close memory search index: %w", err)
	}
	return nil
}
