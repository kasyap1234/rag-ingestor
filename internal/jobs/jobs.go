package jobs

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status represents the current state of a job
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

// Job represents an async processing job
type Job struct {
	ID          string                 `json:"id"`
	Status      Status                 `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Progress    int                    `json:"progress"` // 0-100
	TotalFiles  int                    `json:"total_files"`
	Processed   int                    `json:"processed_files"`
	Results     []JobResult            `json:"results,omitempty"`
	Error       string                 `json:"error,omitempty"`
	WebhookURL  string                 `json:"webhook_url,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// JobResult represents the result of processing a single file
type JobResult struct {
	Filename string      `json:"filename"`
	Success  bool        `json:"success"`
	Content  string      `json:"content,omitempty"`
	Error    string      `json:"error,omitempty"`
	Data     interface{} `json:"data,omitempty"`
}

// Manager handles job lifecycle and storage
type Manager struct {
	jobs    map[string]*Job
	mu      sync.RWMutex
	workers chan struct{}
}

// NewManager creates a new job manager with specified worker count
func NewManager(workerCount int) *Manager {
	if workerCount <= 0 {
		workerCount = 4
	}
	return &Manager{
		jobs:    make(map[string]*Job),
		workers: make(chan struct{}, workerCount),
	}
}

// CreateJob creates a new job and returns its ID
func (m *Manager) CreateJob(totalFiles int, webhookURL string) *Job {
	job := &Job{
		ID:         uuid.New().String(),
		Status:     StatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		TotalFiles: totalFiles,
		WebhookURL: webhookURL,
		Metadata:   make(map[string]interface{}),
	}

	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()

	return job
}

// GetJob retrieves a job by ID
func (m *Manager) GetJob(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	return job, ok
}

// UpdateStatus updates job status
func (m *Manager) UpdateStatus(id string, status Status, err string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if job, ok := m.jobs[id]; ok {
		job.Status = status
		job.UpdatedAt = time.Now()
		if err != "" {
			job.Error = err
		}
		if status == StatusCompleted || status == StatusFailed {
			now := time.Now()
			job.CompletedAt = &now
		}
	}
}

// AddResult adds a processing result to a job
func (m *Manager) AddResult(id string, result JobResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if job, ok := m.jobs[id]; ok {
		job.Results = append(job.Results, result)
		job.Processed++
		job.Progress = (job.Processed * 100) / job.TotalFiles
		job.UpdatedAt = time.Now()
	}
}

// StartProcessing marks a job as processing
func (m *Manager) StartProcessing(id string) {
	m.UpdateStatus(id, StatusProcessing, "")
}

// CompleteJob marks a job as completed
func (m *Manager) CompleteJob(id string) {
	m.UpdateStatus(id, StatusCompleted, "")
}

// FailJob marks a job as failed
func (m *Manager) FailJob(id string, err string) {
	m.UpdateStatus(id, StatusFailed, err)
}

// ListJobs returns all jobs (for debugging)
func (m *Manager) ListJobs() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()

	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// CleanupOldJobs removes jobs older than the specified duration
func (m *Manager) CleanupOldJobs(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	deleted := 0

	for id, job := range m.jobs {
		if job.CreatedAt.Before(cutoff) {
			delete(m.jobs, id)
			deleted++
		}
	}

	return deleted
}

// AcquireWorker tries to acquire a worker slot (non-blocking)
func (m *Manager) AcquireWorker() bool {
	select {
	case m.workers <- struct{}{}:
		return true
	default:
		return false
	}
}

// ReleaseWorker releases a worker slot
func (m *Manager) ReleaseWorker() {
	<-m.workers
}
