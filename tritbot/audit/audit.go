// Checks that the log is append-only.
package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/golang/glog"
	tc "github.com/google/trillian/client"
	tt "github.com/google/trillian/types"
	"github.com/mhutchinson/tritter/tritbot/log"
	"google.golang.org/grpc"
)

var (
	loggerAddr       = flag.String("logger_addr", "localhost:50053", "the address of the trillian logger personality")
	connectTimeout   = flag.Duration("connect_timeout", time.Second, "the timeout for connecting to the server")
	fetchRootTimeout = flag.Duration("fetch_root_timeout", 2*time.Second, "the timeout for fetching the latest log root")

	pollInterval = flag.Duration("poll_interval", 5*time.Second, "how often to audit the log")
)

type auditor struct {
	timeout time.Duration

	log log.LoggerClient
	v   *tc.LogVerifier
	con *grpc.ClientConn

	trustedRoot tt.LogRootV1
}

func new(ctx context.Context) *auditor {
	// Set up a connection to the Logger server.
	lCon, err := grpc.DialContext(ctx, *loggerAddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		glog.Fatalf("could not connect to logger on %v: %v", *loggerAddr, err)
	}
	v, err := log.TreeVerifier()
	if err != nil {
		glog.Fatalf("could not create tree verifier: %v", err)
	}

	return &auditor{
		timeout: *fetchRootTimeout,
		log:     log.NewLoggerClient(lCon),
		v:       v,
		con:     lCon,
	}
}

func (a *auditor) checkLatest(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	r, err := a.log.LatestRoot(ctx, &log.LatestRootRequest{LastTreeSize: int64(a.trustedRoot.TreeSize)})
	if err != nil {
		return err
	}

	proof := [][]byte{{}}
	if a.trustedRoot.TreeSize > 0 {
		proof = r.GetProof().GetHashes()
	}
	newRoot, err := a.v.VerifyRoot(&a.trustedRoot, r.GetRoot(), proof)
	if err != nil {
		return fmt.Errorf("failed to verify log root: %v", err)
	}

	if newRoot.Revision > a.trustedRoot.Revision {
		a.trustedRoot = *newRoot
		glog.Infof("updated trusted root to revision=%d with size=%d", newRoot.Revision, newRoot.TreeSize)
	}

	return nil
}

func (a *auditor) close() error {
	return a.con.Close()
}

func main() {
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *connectTimeout)
	defer cancel()

	a := new(ctx)
	defer a.close()

	glog.Infof("auditor running, poll interval: %v", *pollInterval)
	ticker := time.NewTicker(*pollInterval)
	for {
		select {
		case <-ticker.C:
			glog.V(2).Info("Tick")
			if err := a.checkLatest(context.Background()); err != nil {
				glog.Warningf("error checking latest root: %v", err)
			}
		case <-context.Background().Done():
			glog.Info("context done - finishing")
			ticker.Stop()
			return
		}
	}
}
