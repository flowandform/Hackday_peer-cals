package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/go-chi/chi"
	"github.com/go-chi/jwtauth"
	"github.com/gobuffalo/packr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var tokenAuth *jwtauth.JWTAuth

// var _log *Logger

func init() {
	tokenAuth = jwtauth.New("HS256", []byte("eff1c804d6f348ac978672e725da0e3e8ad6d0c91fa313746cdef90600e733406fb88ba758d8f48e82b974d01e3856394b869279d77d2c6a2b1dc07229fe5e88"), nil)
	// _log := loggerFactory.GetLogger("mux")

	// For debugging/example purposes, we generate and print
	// a sample jwt token with claims `user_id:123` here:
	// _, tokenString, _ := tokenAuth.Encode(jwt.MapClaims{"user_id": 123})
	// fmt.Printf("DEBUG: a sample jwt is %s\n\n", tokenString)
}

func buildManifest(baseURL string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"name":             "Peer Calls",
		"short_name":       "Peer Calls",
		"start_url":        baseURL,
		"display":          "standalone",
		"background_color": "#086788",
		"description":      "Group peer-to-peer calls for everyone. Create a private room. Share the link.",
		"icons": []map[string]string{{
			"src":   baseURL + "/res/icon.png",
			"sizes": "256x256",
			"type":  "image/png",
		}},
	})
	return b
}

type Mux struct {
	BaseURL    string
	handler    *chi.Mux
	iceServers []ICEServer
	network    NetworkConfig
	version    string
}

func (mux *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux.handler.ServeHTTP(w, r)
}

type TracksManager interface {
	Add(room string, transport *WebRTCTransport)
	GetTracksMetadata(room string, clientID string) ([]TrackMetadata, bool)
}

func withGauge(counter prometheus.Counter, h http.HandlerFunc, loggerFactory LoggerFactory, check bool) http.HandlerFunc {
	log := loggerFactory.GetLogger("withGauge")
	return func(w http.ResponseWriter, r *http.Request) {

		if check {
			_, claims, _ := jwtauth.FromContext(r.Context())
			service := claims["service"]
			path := r.URL.Path
			log.Printf("service: %v\n", service)
			// log.Printf("request: %+v\n", r)
			log.Printf("path: %v\n", path)
			doesContain := false
			if service != nil {
				doesContain = strings.Contains(path, service.(string))
			}
			log.Printf("is true: %t\n", doesContain)
			log.Printf("is check: %t\n", check)
			if doesContain {
				counter.Inc()
				h.ServeHTTP(w, r)
			} else {
				w.Write([]byte("fuck off"))
			}
		} else {
			counter.Inc()
			h.ServeHTTP(w, r)
		}

	}
}

type RoomManager interface {
	Enter(room string) Adapter
	Exit(room string)
}

func NewMux(
	loggerFactory LoggerFactory,
	baseURL string,
	version string,
	network NetworkConfig,
	iceServers []ICEServer,
	rooms RoomManager,
	tracks TracksManager,
	prom PrometheusConfig,
) *Mux {
	box := packr.NewBox("./templates")
	templates := ParseTemplates(box)
	renderer := NewRenderer(loggerFactory, templates, baseURL, version)

	handler := chi.NewRouter()
	mux := &Mux{
		BaseURL:    baseURL,
		handler:    handler,
		iceServers: iceServers,
		network:    network,
		version:    version,
	}

	var root string
	if baseURL == "" {
		root = "/"
	} else {
		root = baseURL
	}

	wsHandler := newWebSocketHandler(
		loggerFactory,
		network,
		NewWSS(loggerFactory, rooms),
		iceServers,
		tracks,
	)

	manifest := buildManifest(baseURL)
	handler.Group(func(router chi.Router) {
		router.Use(jwtauth.Verifier(tokenAuth))
		router.Use(jwtauth.Authenticator)
		router.Post("/call", withGauge(prometheusCallJoinTotal, mux.routeNewCall, loggerFactory, true))
		router.Get("/call/{callID}", withGauge(prometheusCallViewsTotal, renderer.Render(mux.routeCall), loggerFactory, true))
	})
	handler.Route(root, func(router chi.Router) {
		router.Get("/", withGauge(prometheusHomeViewsTotal, renderer.Render(mux.routeIndex), loggerFactory, false))
		router.Handle("/static/*", static(baseURL+"/static", packr.NewBox("../build")))
		router.Handle("/res/*", static(baseURL+"/res", packr.NewBox("../res")))
		// router.Post("/call", withGauge(prometheusCallJoinTotal, mux.routeNewCall, loggerFactory, false))
		// router.Get("/call/{callID}", withGauge(prometheusCallViewsTotal, renderer.Render(mux.routeCall), loggerFactory, false))
		router.Get("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(manifest)
		})
		router.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
			accessToken := r.Header.Get("Authorization")
			if strings.HasPrefix(accessToken, "Bearer ") {
				accessToken = accessToken[len("Bearer "):]
			} else {
				accessToken = r.FormValue("access_token")
			}

			if accessToken == "" || accessToken != prom.AccessToken {
				w.WriteHeader(401)
				return
			}
			promhttp.Handler().ServeHTTP(w, r)
		})

		router.Mount("/ws", wsHandler)
	})

	return mux
}

func newWebSocketHandler(
	loggerFactory LoggerFactory,
	network NetworkConfig,
	wss *WSS,
	iceServers []ICEServer,
	tracks TracksManager,
) http.Handler {
	log := loggerFactory.GetLogger("mux")
	switch network.Type {
	case NetworkTypeSFU:
		log.Println("Using network type sfu")
		return NewSFUHandler(loggerFactory, wss, iceServers, network.SFU, tracks)
	default:
		log.Println("Using network type mesh")
		return NewMeshHandler(loggerFactory, wss)
	}
}

func static(prefix string, box packr.Box) http.Handler {
	fileServer := http.FileServer(http.FileSystem(box))
	return http.StripPrefix(prefix, fileServer)
}

func (mux *Mux) routeNewCall(w http.ResponseWriter, r *http.Request) {
	callID := r.PostFormValue("call")
	if callID == "" {
		callID = NewUUIDBase62()
	}
	url := mux.BaseURL + "/call/" + url.PathEscape(callID)
	http.Redirect(w, r, url, 302)
}

func (mux *Mux) routeIndex(w http.ResponseWriter, r *http.Request) (string, interface{}, error) {
	fmt.Printf("serving index")
	return "index.html", nil, nil
}

func (mux *Mux) routeCall(w http.ResponseWriter, r *http.Request) (string, interface{}, error) {
	callID := url.PathEscape(path.Base(r.URL.Path))
	userID := NewUUIDBase62()
	// _, claims, _ := jwtauth.FromContext(r.Context())

	iceServers := GetICEAuthServers(mux.iceServers)
	iceServersJSON, _ := json.Marshal(iceServers)

	data := map[string]interface{}{
		"Nickname":   r.Header.Get("X-Forwarded-User"),
		"CallID":     callID,
		"UserID":     userID,
		"ICEServers": template.HTML(iceServersJSON),
		"Network":    mux.network.Type,
		"Version":    mux.version,
	}
	return "call.html", data, nil
}
