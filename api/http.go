package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/intob/godave"
	"github.com/intob/godave/pow"
	"github.com/intob/godave/store"
)

type Service struct {
	listenAddr string
	logs       chan<- string
	dave       *godave.Dave
}

type Cfg struct {
	ListenAddr string
	Logs       chan<- string
	Dave       *godave.Dave
}

type status struct {
	Peers int    `json:"peers"`
	Dats  uint32 `json:"dats"`
}

type datWorkReq struct {
	Key        string `json:"key"`
	Val        string `json:"val"`
	Time       int64  `json:"time"` // Unix milli
	Difficulty uint8  `json:"difficulty"`
}

type datWorkResp struct {
	Salt string `json:"salt"`
	Work string `json:"work"`
}

type datPutReq struct {
	Key    string `json:"key"`
	Val    string `json:"val"`
	Time   int64  `json:"time"` // Unix milli
	Salt   string `json:"salt"`
	Work   string `json:"work"`
	PubKey string `json:"pubKey"`
	Sig    string `json:"sig"`
}

func NewService(cfg *Cfg) *Service {
	svc := &Service{
		listenAddr: cfg.ListenAddr,
		logs:       cfg.Logs,
		dave:       cfg.Dave,
	}
	http.Handle("/", corsMiddleware(http.HandlerFunc(svc.handleGetStatus)))
	http.Handle("/status", corsMiddleware(http.HandlerFunc(svc.handleGetStatus)))
	http.Handle("/work", corsMiddleware(http.HandlerFunc(svc.handleDoWork)))
	http.Handle("/put", corsMiddleware(http.HandlerFunc(svc.handlePut)))
	http.Handle("/ws", corsMiddleware(http.HandlerFunc(svc.handleWebsocketConnection)))
	return svc
}

func (svc *Service) Start() error {
	errChan := make(chan error, 1)
	go func() {
		err := http.ListenAndServe(svc.listenAddr, nil)
		if err != nil {
			errChan <- err
		}
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-errChan:
		return err
	default:
		svc.log("started http server on %s", svc.listenAddr)
		return nil
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		next.ServeHTTP(w, r)
	})
}

func (svc *Service) handleDoWork(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	req := &datWorkReq{}
	err := dec.Decode(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode request body: %s", err)))
		return
	}
	key, err := base64.RawURLEncoding.DecodeString(req.Key)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode base64 key: %s", err)))
		return
	}
	timeParsed := time.UnixMilli(req.Time)
	work, salt := pow.DoWork(key, []byte(req.Val), pow.Ttb(timeParsed), req.Difficulty)
	resp := &datWorkResp{
		Salt: base64.RawURLEncoding.EncodeToString(salt),
		Work: base64.RawURLEncoding.EncodeToString(work),
	}
	respJson, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("failed to marshal response json: %s", err)))
		return
	}
	w.Write(respJson)
}

func (svc *Service) handlePut(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	req := &datPutReq{}
	err := dec.Decode(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode request body: %s", err)))
		return
	}
	key, err := base64.RawURLEncoding.DecodeString(req.Key)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode base64 key: %s", err)))
		return
	}
	salt, err := base64.RawURLEncoding.DecodeString(req.Salt)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode base64 salt: %s", err)))
		return
	}
	work, err := base64.RawURLEncoding.DecodeString(req.Work)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode base64 work: %s", err)))
		return
	}
	pubKey, err := base64.RawURLEncoding.DecodeString(req.PubKey)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode base64 pub key: %s", err)))
		return
	}
	sig, err := base64.RawURLEncoding.DecodeString(req.Sig)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode base64 sig: %s", err)))
		return
	}
	<-svc.dave.Put(&store.Dat{
		Key:    key,
		Val:    []byte(req.Val),
		Time:   time.UnixMilli(req.Time),
		Salt:   salt,
		Work:   work,
		PubKey: ed25519.PublicKey(pubKey),
		Sig:    sig,
	})
}

func (svc *Service) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	stat := &status{
		Peers: svc.dave.Peers.Count(),
		Dats:  svc.dave.Store.Count(),
	}
	resp, err := json.MarshalIndent(stat, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	w.Write(resp)
}

func (svc *Service) log(msg string, args ...any) {
	svc.logs <- fmt.Sprintf("/api "+msg, args...)
}
