package pgstore

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRegistration(t *testing.T) {
	var (
		user  = "myuser"
		host  = "myhost"
		name  = "mydbname"
		count = 5
	)
	for i := 1; i <= count; i++ {
		c := newPoolCollector(user, host, name, func() stat { return &pgxStatMock{} })
		if err := prometheus.Register(c); err != nil {
			t.Errorf("Register %d/%d: %v", i, count, err)
		}
	}
}

type pgxStatMock struct {
	acquireCount         int64
	acquireDuration      time.Duration
	canceledAcquireCount int64
	emptyAcquireCount    int64
	acquiredConns        int32
	constructingConns    int32
	idleConns            int32
	maxConns             int32
	totalConns           int32
}

var _ stat = (*pgxStatMock)(nil)

func (m *pgxStatMock) AcquireCount() int64            { return m.acquireCount }
func (m *pgxStatMock) AcquireDuration() time.Duration { return m.acquireDuration }
func (m *pgxStatMock) AcquiredConns() int32           { return m.acquiredConns }
func (m *pgxStatMock) CanceledAcquireCount() int64    { return m.canceledAcquireCount }
func (m *pgxStatMock) ConstructingConns() int32       { return m.constructingConns }
func (m *pgxStatMock) EmptyAcquireCount() int64       { return m.emptyAcquireCount }
func (m *pgxStatMock) IdleConns() int32               { return m.idleConns }
func (m *pgxStatMock) MaxConns() int32                { return m.maxConns }
func (m *pgxStatMock) TotalConns() int32              { return m.totalConns }
