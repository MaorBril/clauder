package store

import (
	"time"
)

type Fact struct {
	ID        int64      `json:"id"`
	Content   string     `json:"content"`
	Tags      []string   `json:"tags,omitempty"`
	SourceDir string     `json:"source_dir"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type BulkFact struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
}

type DirStats struct {
	Count  int       `json:"count"`
	Size   int       `json:"size"`
	Oldest time.Time `json:"oldest"`
	Newest time.Time `json:"newest"`
}

type FactStats struct {
	TotalFacts   int                `json:"total_facts"`
	TotalSize    int                `json:"total_size"`
	DeletedFacts int                `json:"deleted_facts"`
	DeletedSize  int                `json:"deleted_size"`
	ByDirectory  map[string]DirStats `json:"by_directory"`
}

type Instance struct {
	ID            string    `json:"id"`
	DirectoryID   string    `json:"directory_id"`
	Name          string    `json:"name,omitempty"`
	PID           int       `json:"pid"`
	Directory     string    `json:"directory"`
	TTY           string    `json:"tty,omitempty"`
	IsLeader      bool      `json:"is_leader"`
	IsIdle        bool      `json:"is_idle"`
	StartedAt     time.Time `json:"started_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

type Message struct {
	ID           int64      `json:"id"`
	FromInstance string     `json:"from_instance"`
	ToInstance   string     `json:"to_instance"`
	Content      string     `json:"content"`
	CreatedAt    time.Time  `json:"created_at"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
}

type Store interface {
	// Facts
	AddFact(content string, tags []string, sourceDir string) (*Fact, error)
	BulkAddFacts(facts []BulkFact, sourceDir string) ([]Fact, error)
	GetFacts(query string, tags []string, sourceDir string, limit int) ([]Fact, error)
	GetFactByID(id int64) (*Fact, error)
	GetAllFactsByDir(sourceDir string) ([]Fact, error)
	GetAllFacts() ([]Fact, error)
	DeleteFact(id int64) error
	SoftDeleteFact(id int64) error
	BulkSoftDeleteFacts(ids []int64) (int, error)
	UpdateFact(id int64, content string, tags []string) (*Fact, error)
	CompressFacts(deleteIDs []int64, newFacts []BulkFact, sourceDir string) (int, []Fact, error)
	PurgeDeletedFacts() (int, error)
	GetFactStats() (*FactStats, error)

	// Instances
	RegisterInstance(id, directoryID, name, directory, tty string, pid int) error
	Heartbeat(id string) error
	UnregisterInstance(id string) error
	GetInstances() ([]Instance, error)
	GetInstance(id string) (*Instance, error)
	GetInstancesByDirectory(directoryID string) ([]Instance, error)
	CheckDirectoryHasActiveInstance(directoryID string) (bool, error)
	CleanupStaleInstances(maxAge time.Duration) error

	// Leader election
	TryBecomeLeader(id string) (bool, error)
	ReleaseLeadership(id string) error
	GetLeader() (*Instance, error)

	// Idle tracking
	SetIdle(id string, idle bool) error
	GetIdleInstancesWithUnreadMessages() ([]Instance, error)

	// Messages
	SendMessage(from, to, content string) (*Message, error)
	GetMessages(toInstance string, unreadOnly bool) ([]Message, error)
	GetAllMessages(limit int) ([]Message, error)
	MarkMessageRead(id int64) error

	// Lifecycle
	Close() error
}
