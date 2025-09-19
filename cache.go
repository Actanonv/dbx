package db

import (
	"log/slog"
	"sync"
	"time"

	"fmt"
	"github.com/uptrace/bun"
)

type Cache struct {
	mu           sync.Mutex
	cache        map[string]*bun.DB
	lastAccessed map[string]time.Time
	quit         chan struct{}
}

func (c *Cache) Has(name string) *bun.DB {
	c.mu.Lock()
	defer c.mu.Unlock()

	db, found := c.cache[name]
	if !found {
		return nil
	}
	return db
}

func (c *Cache) Get(name string) (db *bun.DB, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var found bool
	if db, found = c.cache[name]; !found {
		return nil, fmt.Errorf("database %s not found in cache", name)
	}

	c.lastAccessed[name] = time.Now()
	return db, nil
}

func (c *Cache) GetOrOpen(name string) (db *bun.DB, err error) {
	c.mu.Lock()
	defer func() {
		if err == nil {
			c.lastAccessed[name] = time.Now()
		}

		c.mu.Unlock()
	}()

	if db, found := c.cache[name]; found {
		return db, nil
	}

	if db, err = OpenDB(name); err != nil {
		return nil, err
	}

	c.cache[name] = db
	return db, nil
}

func (c *Cache) Set(name string, db *bun.DB) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, found := c.cache[name]; found {
		return false
	}

	c.cache[name] = db
	c.lastAccessed[name] = time.Now()
	return true
}

func (c *Cache) Close() error {
	close(c.quit)
	return nil
}

const maxInactiveDuration = 30 * time.Minute

func (c *Cache) Cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.quit:
			return
		case <-ticker.C:
			c.mu.Lock()
			for name, lastAccess := range c.lastAccessed {
				if time.Since(lastAccess) > maxInactiveDuration {
					if db, ok := c.cache[name]; ok {
						if db != nil {
							if err := db.Close(); err != nil {
								slog.Error("sqlDB.Close()", "err", err.Error())
							}
						}
					}

					delete(c.lastAccessed, name)
					delete(c.cache, name)
				}
			}
			c.mu.Unlock()
		}
	}
}
