package gst

/*
#cgo pkg-config: gstreamer-1.0 gstreamer-app-1.0

#include "gst.h"

*/
import "C"
import (
	"io"
	"sync"
	"unsafe"

	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"
)

func init() {
	go C.gstreamer_send_start_mainloop()
}

// Pipeline is a wrapper for a GStreamer Pipeline
type Pipeline struct {
	Pipeline   *C.GstElement
	audioTrack *webrtc.Track
	videoTrack *webrtc.Track
}

var pipeline = &Pipeline{}
var pipelinesLock sync.Mutex

// This is the frequency and program number for KCTS 9 (PBS) in Seattle, WA.
const pipelineStr = `
	dvbsrc delsys=atsc modulation=8vsb frequency=189000000
	! tsdemux name=demux program-number=3

	demux.
	! queue leaky=downstream max-size-time=2500000000 max-size-buffers=0 max-size-bytes=0
	! decodebin
	! videoconvert
	! videoscale
	! video/x-raw,width=853,height=480
	! vp8enc deadline=1
	! appsink name=video

	demux.
	! queue leaky=downstream max-size-time=2500000000 max-size-buffers=0 max-size-bytes=0
	! decodebin
	! audioconvert
	! audioresample
	! audio/x-raw,rate=48000
	! opusenc bitrate=128000
	! appsink name=audio
`

// CreatePipeline creates a GStreamer Pipeline
func CreatePipeline(audioTrack, videoTrack *webrtc.Track) *Pipeline {
	pipelineStrUnsafe := C.CString(pipelineStr)
	defer C.free(unsafe.Pointer(pipelineStrUnsafe))

	pipelinesLock.Lock()
	defer pipelinesLock.Unlock()
	pipeline = &Pipeline{
		Pipeline:   C.gstreamer_send_create_pipeline(pipelineStrUnsafe),
		audioTrack: audioTrack,
		videoTrack: videoTrack,
	}
	return pipeline
}

// Start starts the GStreamer Pipeline
func (p *Pipeline) Start() {
	// This will signal to goHandlePipelineBuffer
	// and provide a method for cancelling sends.
	C.gstreamer_send_start_pipeline(p.Pipeline)
}

const (
	videoClockRate = 90000
	audioClockRate = 48000
)

//export goHandlePipelineBuffer
func goHandlePipelineBuffer(buffer unsafe.Pointer, bufferLen C.int, duration C.int, isVideo C.int) {
	var track *webrtc.Track
	var samples uint32

	if isVideo == 1 {
		samples = uint32(videoClockRate * (float32(duration) / 1000000000))
		track = pipeline.videoTrack
	} else {
		samples = uint32(audioClockRate * (float32(duration) / 1000000000))
		track = pipeline.audioTrack
	}

	if err := track.WriteSample(media.Sample{Data: C.GoBytes(buffer, bufferLen), Samples: samples}); err != nil && err != io.ErrClosedPipe {
		panic(err)
	}

	C.free(buffer)
}
