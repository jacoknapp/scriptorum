package httpapi

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

type approvalJob struct {
	id       int64
	req      *db.Request
	inst     providers.ReadarrInstance
	username string
}

var errApprovalQueueFull = errors.New("approval queue is full")

func approvalQueueCapacity(interval, jitter, maxWait time.Duration) int {
	if maxWait <= 0 {
		return 1
	}
	worstDelay := interval + jitter
	if worstDelay <= 0 {
		return 1
	}
	capacity := int(maxWait / worstDelay)
	if capacity < 1 {
		return 1
	}
	return capacity
}

func (s *Server) enqueueAsyncApproval(id int64, req *db.Request, inst providers.ReadarrInstance, username string) error {
	s.approvalQueueOnce.Do(func() {
		go s.runApprovalQueue()
	})

	job := approvalJob{
		id:       id,
		req:      req,
		inst:     inst,
		username: username,
	}

	select {
	case s.approvalQueue <- job:
		return nil
	default:
		return fmt.Errorf("%w; try again after existing approvals drain", errApprovalQueueFull)
	}
}

func (s *Server) runApprovalQueue() {
	var lastStarted time.Time
	for job := range s.approvalQueue {
		if !lastStarted.IsZero() {
			if wait := time.Until(lastStarted.Add(s.nextApprovalQueueDelay())); wait > 0 {
				time.Sleep(wait)
			}
		}
		lastStarted = time.Now()
		s.processAsyncApproval(job.id, job.req, job.inst, job.username)
	}
}

func (s *Server) nextApprovalQueueDelay() time.Duration {
	delay := s.approvalQueueInterval
	jitter := s.approvalQueueJitter
	if jitter <= 0 {
		return delay
	}
	return delay + time.Duration(rand.Int63n(int64(jitter)+1))
}
