package dbx

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"
)

var (
	// ErrCacheClosed is returned when an operation is attempted on a closed Cache.
	ErrCacheClosed = errors.New("cache is closed")
	// ErrDatabaseNotFound is returned when a requested database key does not exist in the Cache.
	ErrDatabaseNotFound = errors.New("database not found in cache")
	// ErrDatabaseOpenFailed is returned when opening a database fails across concurrent waiting goroutines.
	ErrDatabaseOpenFailed = errors.New("database failed to open in another goroutine")
)

// Cache manages a thread-safe pool of database connections with automatic background cleanup
// of inactive connections and double-checked locking to protect against thundering herds.
type Cache struct {
	mu               sync.Mutex
	cache            map[string]*bun.DB
	lastAccessed     map[string]time.Time
	opening          map[string]chan struct{} // channels for per-key locking
	quit             chan struct{}
	closeOnce        sync.Once
	inactiveDuration time.Duration
}

// NewCache creates and starts a new connection cache.
// Inactive database connections that have not been accessed for longer than
// inactiveDuration are automatically closed and evicted.
func NewCache(inactiveDuration time.Duration) *Cache {
	c := &Cache{
		mu:               sync.Mutex{},
		cache:            make(map[string]*bun.DB),
		lastAccessed:     make(map[string]time.Time),
		opening:          make(map[string]chan struct{}),
		quit:             make(chan struct{}),
		inactiveDuration: inactiveDuration,
	}

	go c.Cleanup()

	return c
}

// Has checks if a database with the given name is currently active in the cache without updating its last accessed time.
// Returns the *bun.DB instance if found, or nil if not present or if the cache is closed.
func (c *Cache) Has(name string) *bun.DB {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.quit:
		return nil
	default:
	}

	db, found := c.cache[name]
	if !found {
		return nil
	}
	return db
}

// Get retrieves an existing database connection from the cache and updates its last accessed time.
// Returns ErrDatabaseNotFound if the connection is not currently in the cache.
func (c *Cache) Get(name string) (db *bun.DB, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.quit:
		return nil, ErrCacheClosed
	default:
	}

	var found bool
	if db, found = c.cache[name]; !found {
		return nil, fmt.Errorf("%w: %s", ErrDatabaseNotFound, name)
	}

	c.lastAccessed[name] = time.Now()
	return db, nil
}

// GetOrOpen retrieves an existing database connection from the cache, or opens and caches
// a new connection using OpenDB if not already present.
// It uses per-key locking so that concurrent requests for the same database name only
// open the database once (preventing thundering herd).
func (c *Cache) GetOrOpen(name string, openOptions ...OpenOptFn) (db *bun.DB, err error) {
	c.mu.Lock()
	select {
	case <-c.quit:
		c.mu.Unlock()
		return nil, ErrCacheClosed
	default:
	}

	if db, found := c.cache[name]; found {
		c.lastAccessed[name] = time.Now()
		c.mu.Unlock()
		return db, nil
	}

	// Double-checked locking using a per-key channel for blocking
	waitCh, isOpening := c.opening[name]
	if isOpening {
		// Another goroutine is already opening this DB.
		// Release the global lock and wait on the per-key channel or quit.
		c.mu.Unlock()
		select {
		case <-waitCh:
		case <-c.quit:
			return nil, fmt.Errorf("%w while waiting for database %s", ErrCacheClosed, name)
		}

		// After waiting, check the cache again
		c.mu.Lock()
		defer c.mu.Unlock()

		select {
		case <-c.quit:
			return nil, ErrCacheClosed
		default:
		}

		if db, found := c.cache[name]; found {
			c.lastAccessed[name] = time.Now()
			return db, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrDatabaseOpenFailed, name)
	}

	// This goroutine will perform the opening.
	// Create a channel and register it in the opening map.
	waitCh = make(chan struct{})
	c.opening[name] = waitCh
	c.mu.Unlock()

	// Perform the potentially slow OpenDB operation without holding the global lock.
	defer func() {
		c.mu.Lock()
		delete(c.opening, name)
		close(waitCh)
		c.mu.Unlock()
	}()

	if db, err = OpenDB(name, openOptions...); err != nil {
		return nil, err
	}

	c.mu.Lock()
	select {
	case <-c.quit:
		c.mu.Unlock()
		if db != nil {
			_ = db.Close()
		}
		return nil, fmt.Errorf("%w during opening", ErrCacheClosed)
	default:
	}

	c.cache[name] = db
	c.lastAccessed[name] = time.Now()
	c.mu.Unlock()

	return db, nil
}

// Set manually stores a bun.DB instance in the cache.
// Returns false if the key already exists or if the cache is closed.
func (c *Cache) Set(name string, db *bun.DB) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.quit:
		return false
	default:
	}

	if _, found := c.cache[name]; found {
		return false
	}

	c.cache[name] = db
	c.lastAccessed[name] = time.Now()
	return true
}

// Delete closes and removes a specific database connection from the cache.
// Returns ErrCacheClosed if the cache is closed, or nil if deleted or not found.
func (c *Cache) Delete(name string) error {
	c.mu.Lock()
	select {
	case <-c.quit:
		c.mu.Unlock()
		return ErrCacheClosed
	default:
	}

	db, found := c.cache[name]
	if !found {
		c.mu.Unlock()
		return nil
	}

	delete(c.cache, name)
	delete(c.lastAccessed, name)
	c.mu.Unlock()

	if db != nil {
		return db.Close()
	}
	return nil
}

// Close gracefully closes all open database connections in the cache and stops the cleanup goroutine.
func (c *Cache) Close() error {
	c.closeOnce.Do(func() {
		close(c.quit)

		c.mu.Lock()
		dbs := make([]*bun.DB, 0, len(c.cache))
		for _, db := range c.cache {
			if db != nil {
				dbs = append(dbs, db)
			}
		}
		// Clear maps
		c.cache = make(map[string]*bun.DB)
		c.lastAccessed = make(map[string]time.Time)
		c.mu.Unlock()

		// Close databases outside the lock
		for _, db := range dbs {
			if err := db.Close(); err != nil {
				slog.Error("sqlDB.Close() on shutdown", "err", err.Error())
			}
		}
	})
	return nil
}

// Cleanup runs a background loop to evict connections that have exceeded the inactive duration.
func (c *Cache) Cleanup() {
	// Use 1/10th of inactiveDuration for ticker, but at least 1 second and at most 1 minute
	tickDuration := c.inactiveDuration / 10
	if tickDuration < time.Second {
		tickDuration = time.Second
	}
	if tickDuration > time.Minute {
		tickDuration = time.Minute
	}

	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	for {
		select {
		case <-c.quit:
			return
		case <-ticker.C:
			c.mu.Lock()
			var toClose []struct {
				name string
				db   *bun.DB
			}

			now := time.Now()
			for name, lastAccess := range c.lastAccessed {
				if now.Sub(lastAccess) > c.inactiveDuration {
					if db, ok := c.cache[name]; ok {
						toClose = append(toClose, struct {
							name string
							db   *bun.DB
						}{name, db})
					}
					delete(c.lastAccessed, name)
					delete(c.cache, name)
				}
			}
			c.mu.Unlock()

			// Close outside the lock to avoid HOL blocking
			for _, item := range toClose {
				if item.db != nil {
					if err := item.db.Close(); err != nil {
						slog.Error("sqlDB.Close() during cleanup", "name", item.name, "err", err.Error())
					}
				}
			}
		}
	}
}
