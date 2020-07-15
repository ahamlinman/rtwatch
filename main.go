package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/pion/rtwatch/gst"
	"github.com/pion/webrtc/v2"
)

const homeHTML = `<!DOCTYPE html>
<html lang="en">
	<head>
		<title>dvbsrc WebRTC Demo</title>
	</head>
	<body id="body">
		<video id="video1" autoplay playsinline></video>

		<script>
			let pc = new RTCPeerConnection();
			pc.addTransceiver('video', {direction: 'recvonly'});
			pc.addTransceiver('audio', {direction: 'recvonly'});

			pc.ontrack = function (event) {
			  if (event.track.kind === 'audio') {
					return;
			  }
			  var el = document.getElementById('video1');
			  el.srcObject = event.streams[0];
			  el.autoplay = true;
			  el.controls = true;
			};

			let conn = new WebSocket('ws://' + window.location.host + '/ws');
			window.conn = conn;

			conn.onopen = () => {
				console.log('Connection opened');
			};

			conn.onclose = evt => {
				console.log('Connection closed');
			};

			conn.onmessage = evt => {
				let msg = JSON.parse(evt.data);
				if (!msg) {
					return console.log('failed to parse msg');
				}

				switch (msg.event) {
				case 'offer':
					offer = JSON.parse(msg.data);
					if (!offer) {
						return console.log('failed to parse offer');
					}
					console.log('Received offer', offer);
					(async () => {
						pc.setRemoteDescription(offer);
						const answer = await pc.createAnswer();
						await pc.setLocalDescription(answer);
						console.log('Sending answer', answer);
						conn.send(JSON.stringify({event: 'answer', data: JSON.stringify(answer)}));
					})();
				}
			};
		</script>
	</body>
</html>
`

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	peerConnectionConfig = webrtc.Configuration{}

	audioTrack = &webrtc.Track{}
	videoTrack = &webrtc.Track{}
	pipeline   = &gst.Pipeline{}
)

type websocketMessage struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

func main() {
	httpListenAddress := ""
	flag.StringVar(&httpListenAddress, "http-listen-address", ":8080", "address for HTTP server to listen on")
	flag.Parse()

	log.Println("Initializing WebRTC PeerConnection")
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Initializing WebRTC tracks")
	videoTrack, err = pc.NewTrack(webrtc.DefaultPayloadTypeVP8, 5000, "sync", "sync")
	if err != nil {
		log.Fatal(err)
	}

	audioTrack, err = pc.NewTrack(webrtc.DefaultPayloadTypeOpus, 5001, "sync", "sync")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating and starting pipeline")
	pipeline = gst.CreatePipeline(audioTrack, videoTrack)
	pipeline.Start()

	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", serveWs)

	log.Printf("Television is now available on '%s', have fun!\n", httpListenAddress)
	log.Fatal(http.ListenAndServe(httpListenAddress, nil))
}

func handleWebsocketMessage(pc *webrtc.PeerConnection, ws *websocket.Conn, message *websocketMessage) error {
	switch message.Event {
	case "answer":
		answer := webrtc.SessionDescription{}
		if err := json.Unmarshal([]byte(message.Data), &answer); err != nil {
			return err
		}

		if err := pc.SetRemoteDescription(answer); err != nil {
			return err
		}
	}
	return nil
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			log.Println(err)
		}
		return
	}
	defer ws.Close()

	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		log.Print(err)
		return
	} else if _, err = peerConnection.AddTrack(audioTrack); err != nil {
		log.Print(err)
		return
	} else if _, err = peerConnection.AddTrack(videoTrack); err != nil {
		log.Print(err)
		return
	}

	defer func() {
		if err := peerConnection.Close(); err != nil {
			log.Println(err)
		}
	}()

	sdp, err := peerConnection.CreateOffer(nil)
	if err != nil {
		log.Print(err)
		return
	}

	if err := peerConnection.SetLocalDescription(sdp); err != nil {
		log.Print(err)
		return
	}

	sdpData, err := json.Marshal(sdp)
	if err != nil {
		log.Print(err)
		return
	}

	offerMsg := &websocketMessage{
		Event: "offer",
		Data:  string(sdpData),
	}
	if err := ws.WriteJSON(offerMsg); err != nil {
		log.Print(err)
		return
	}

	message := &websocketMessage{}
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		} else if err := json.Unmarshal(msg, &message); err != nil {
			log.Print(err)
			return
		}

		if err := handleWebsocketMessage(peerConnection, ws, message); err != nil {
			log.Print(err)
		}
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, homeHTML)
}
