// Package grpcserver implements the ConfigStream gRPC service consumed by
// applications. It serves a snapshot followed by a live stream of changes and
// heartbeats, fanning out cross-replica changes via the events broker.
package grpcserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/dynoconf/dynoconf/internal/events"
	pb "github.com/dynoconf/dynoconf/internal/grpcserver/configpb"
	"github.com/dynoconf/dynoconf/internal/store"
)

func newConnID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// heartbeatEvery is how often a Heartbeat is pushed when idle.
const heartbeatEvery = 15 * time.Second

// Server implements pb.ConfigStreamServer.
type Server struct {
	pb.UnimplementedConfigStreamServer
	store   *store.Store
	broker  *events.Broker
	tracker *ConnTracker
	log     *slog.Logger
}

// New builds the gRPC service implementation.
func New(st *store.Store, broker *events.Broker, tracker *ConnTracker, log *slog.Logger) *Server {
	return &Server{store: st, broker: broker, tracker: tracker, log: log}
}

// Subscribe streams the snapshot + change feed for the service identified by
// service_key.
func (s *Server) Subscribe(req *pb.SubscribeRequest, stream pb.ConfigStream_SubscribeServer) error {
	ctx := stream.Context()

	if req.GetServiceKey() == "" {
		return status.Error(codes.InvalidArgument, "service_key is required")
	}
	// NOTE: client_token is reserved for future per-service auth and is not
	// validated in v1 (the gRPC endpoint is network-restricted).

	svc, err := s.store.GetServiceByKey(ctx, req.GetServiceKey())
	if errors.Is(err, store.ErrNotFound) {
		return status.Errorf(codes.NotFound, "unknown service_key")
	}
	if err != nil {
		s.log.Error("lookup service failed", "err", err)
		return status.Error(codes.Internal, "internal error")
	}

	// Subscribe BEFORE reading the snapshot so we don't miss changes that land
	// in the gap between snapshot and stream setup. Buffered duplicates are
	// harmless: clients apply upserts idempotently by version.
	evCh, unsubscribe := s.broker.Subscribe(svc.ID)
	defer unsubscribe()

	connID := newConnID()
	peerAddr := "unknown"
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		peerAddr = p.Addr.String()
	}
	s.tracker.Register(svc.ID, connID, peerAddr)
	defer s.tracker.Unregister(svc.ID, connID)

	s.log.Info("stream opened", "service", svc.Key, "send_snapshot", req.GetSendSnapshot())

	if req.GetSendSnapshot() {
		if err := s.sendSnapshot(ctx, svc.ID, stream); err != nil {
			return err
		}
	}

	hb := time.NewTicker(heartbeatEvery)
	defer hb.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info("stream closed", "service", svc.Key)
			return ctx.Err()
		case ev, ok := <-evCh:
			if !ok {
				return status.Error(codes.Unavailable, "server draining")
			}
			if ev.Kind != events.KindVar {
				continue
			}
			if err := s.sendChange(stream, ev); err != nil {
				return err
			}
		case <-hb.C:
			if err := stream.Send(&pb.ConfigEvent{
				Event: &pb.ConfigEvent_Heartbeat{
					Heartbeat: &pb.Heartbeat{Ts: time.Now().Unix()},
				},
			}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) sendSnapshot(ctx context.Context, serviceID int64, stream pb.ConfigStream_SubscribeServer) error {
	vars, err := s.store.ListVariables(ctx, serviceID)
	if err != nil {
		s.log.Error("snapshot load failed", "err", err)
		return status.Error(codes.Internal, "snapshot failed")
	}
	pbvars := make([]*pb.Variable, 0, len(vars))
	for _, v := range vars {
		pbvars = append(pbvars, &pb.Variable{Key: v.Key, Value: v.Value, Version: v.Version})
	}
	return stream.Send(&pb.ConfigEvent{
		Event: &pb.ConfigEvent_Snapshot{Snapshot: &pb.Snapshot{Variables: pbvars}},
	})
}

func (s *Server) sendChange(stream pb.ConfigStream_SubscribeServer, ev events.Event) error {
	t := pb.Change_UPSERT
	if ev.ChangeType == events.Delete {
		t = pb.Change_DELETE
	}
	return stream.Send(&pb.ConfigEvent{
		Event: &pb.ConfigEvent_Change{
			Change: &pb.Change{
				Type:     t,
				Variable: &pb.Variable{Key: ev.Key, Value: ev.Value, Version: ev.Version},
			},
		},
	})
}
