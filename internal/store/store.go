package store

import (
	"time"
)

type Fact struct {
	ID        int64     `json:"id"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	SourceDir string    `json:"source_dir"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Instance struct {
	ID            string    `json:"id"`
	PID           int       `json:"pid"`
	Directory     string    `json:"directory"`
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
	GetFacts(query string, tags []string, sourceDir string, limit int) ([]Fact, error)
	GetFactByID(id int64) (*Fact, error)
	DeleteFact(id int64) error

	// Instances
	RegisterInstance(id string, pid int, directory string) error
	Heartbeat(id string) error
	UnregisterInstance(id string) error
	GetInstances() ([]Instance, error)
	GetInstance(id string) (*Instance, error)
	CleanupStaleInstances(maxAge time.Duration) error

	// Messages
	SendMessage(from, to, content string) (*Message, error)
	GetMessages(toInstance string, unreadOnly bool) ([]Message, error)
	MarkMessageRead(id int64) error

	// Lifecycle
	Close() error
}
