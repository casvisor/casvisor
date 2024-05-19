package routers

import (
	"context"
	"sync"
	"time"

	beegoContext "github.com/beego/beego/context"
	"github.com/casvisor/casvisor/object"
)

var (
	lastAPITime             time.Time
	isUpdateInProgress      bool
	isUpdateInProgressMutex sync.Mutex
	interval                = 30 * time.Second
)

func MetricsFilter(ctx *beegoContext.Context) {
	lastAPITime = time.Now()

	isUpdateInProgressMutex.Lock()
	defer isUpdateInProgressMutex.Unlock()
	if !isUpdateInProgress {
		isUpdateInProgress = true
		go updateAssetMetrics()
	}
}

func updateAssetMetrics() {
	defer func() {
		isUpdateInProgressMutex.Lock()
		isUpdateInProgress = false
		isUpdateInProgressMutex.Unlock()
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case <-ticker.C:
				if time.Since(lastAPITime) > interval {
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			object.CloseSshClients()
			return
		default:
			object.RunUpdateAssetMetrics()
		}
	}
}
