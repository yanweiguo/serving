package statforwarder

import (
	gorillawebsocket "github.com/gorilla/websocket"
	"go.uber.org/zap"
	"knative.dev/pkg/websocket"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

// bucketProcessor includes the information about how to process
// the StatMessage owned by a bucket.
type bucketProcessor struct {
	bktName string
	// holder is the HolderIdentity for a bucket from the Lease.
	holder string
	// conn is the WebSocket connection to the holder pod.
	conn *websocket.ManagedConnection
	// proc is the function to process the StatMessage owned by the bucket.
	accept statProcessor
	logger *zap.SugaredLogger
}

func (p *bucketProcessor) process(sm asmetrics.StatMessage) {
	l := p.logger.With(zap.String("revision", sm.Key.String()))

	if p.accept != nil {
		l.Info("## Accept stat as owner of bucket ", p.bktName)
		p.accept(sm)
		return
	}

	l.Infof("## Forward stat of bucket %s to the holder %s", p.bktName, p.holder)
	wsms := asmetrics.ToWireStatMessages([]asmetrics.StatMessage{sm})
	b, err := wsms.Marshal()
	if err != nil {
		l.Errorw("Error while marshaling stats", zap.Error(err))
		return
	}

	if err := p.conn.SendRaw(gorillawebsocket.BinaryMessage, b); err != nil {
		l.Errorw("Error while sending stats", zap.Error(err))
	}
}
