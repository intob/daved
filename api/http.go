package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/intob/godave"
	"github.com/intob/godave/pow"
	"github.com/intob/godave/types"
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
	Peers int32  `json:"peers"`
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

type datEntry struct {
	Key    string `json:"key"`
	Val    string `json:"val"`
	Time   int64  `json:"time"` // Unix milli
	Salt   string `json:"salt"`
	Work   string `json:"work"`
	PubKey string `json:"pubKey"`
	Sig    string `json:"sig"`
}

type datListReq struct {
	KeyPrefix string `json:"keyPrefix"`
	PubKey    string `json:"pubKey"`
}

type datListResp struct {
	Results []*datEntry `json:"results"`
	Count   int         `json:"count"`
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
	http.Handle("/put", corsMiddleware(http.HandlerFunc(svc.handlePostPut)))
	http.Handle("/list", corsMiddleware(http.HandlerFunc(svc.handlePostList)))
	http.Handle("/ws", corsMiddleware(http.HandlerFunc(svc.handleWebsocketConnection)))
	return svc
}

func (svc *Service) Start() error {
	errChan := make(chan error, 1)
	addrChan := make(chan string, 1)
	go func() {
		listener, err := net.Listen("tcp", svc.listenAddr)
		if err != nil {
			listener, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				errChan <- err
				return
			}
		}
		addrChan <- listener.Addr().String()
		if err := http.Serve(listener, nil); err != nil {
			errChan <- err
		}
	}()
	select {
	case err := <-errChan:
		return err
	case addr := <-addrChan:
		svc.listenAddr = addr
		svc.log("started http server on %s", addr)
		return nil
	case <-time.After(50 * time.Millisecond):
		return fmt.Errorf("timeout waiting for server to start")
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
	timeParsed := time.UnixMilli(req.Time)
	work, salt := pow.DoWork(req.Key, []byte(req.Val), timeParsed, req.Difficulty)
	resp := &datWorkResp{
		Salt: base64.RawURLEncoding.EncodeToString(salt[:]),
		Work: base64.RawURLEncoding.EncodeToString(work[:]),
	}
	respJson, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("failed to marshal response json: %s", err)))
		return
	}
	w.Write(respJson)
}

func (svc *Service) handlePostPut(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	req := &datEntry{}
	err := dec.Decode(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode request body: %s", err)))
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
	err = svc.dave.Put(types.Dat{
		Key:    req.Key,
		Val:    []byte(req.Val),
		Time:   time.UnixMilli(req.Time),
		Salt:   types.Salt(salt),
		Work:   types.Hash(work),
		PubKey: ed25519.PublicKey(pubKey),
		Sig:    types.Signature(sig),
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	}
}

func (svc *Service) handlePostList(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	req := &datListReq{}
	err := dec.Decode(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode request body: %s", err)))
		return
	}
	pubKey, err := base64.RawURLEncoding.DecodeString(req.PubKey)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("failed to decode base64 pub key: %s", err)))
		return
	}
	results := svc.dave.Store.List(ed25519.PublicKey(pubKey), req.KeyPrefix)
	resultsConverted := make([]*datEntry, 0, len(results))
	for _, result := range results {
		resultsConverted = append(resultsConverted, &datEntry{
			Key:    result.Key,
			Val:    base64.RawURLEncoding.EncodeToString(result.Val),
			Salt:   base64.RawURLEncoding.EncodeToString(result.Salt[:]),
			Time:   result.Time.UnixMilli(),
			Work:   base64.RawURLEncoding.EncodeToString(result.Work[:]),
			PubKey: base64.RawURLEncoding.EncodeToString(result.PubKey),
			Sig:    base64.RawURLEncoding.EncodeToString(result.Sig[:]),
		})
	}
	resp := &datListResp{
		Results: resultsConverted,
		Count:   len(results),
	}
	marshalled, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(marshalled)
}

func (svc *Service) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	stat := &status{
		//Peers: svc.dave.Peers.Count(),
		Dats: svc.dave.Store.Count(),
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
