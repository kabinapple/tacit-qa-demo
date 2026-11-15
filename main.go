// tacit-qa-demo: 데모 화면(web/index.html)을 내 컴퓨터 안에서만 띄우는 실행파일.
//
// 하는 일
//  1. web 폴더를 실행파일 안에 넣어 두고, 127.0.0.1(내 컴퓨터)에서만 열리는 서버를 띄운다.
//  2. 기본 브라우저로 데모 화면을 연다.
//  3. .env에 AI 설정이 있으면 /api/ask로 AI에 물을 수 있게 한다. 없으면 AI는 꺼진 채로 동작한다.
//
// API 키는 실행파일이나 저장소에 넣지 않는다. 실행하는 사람이 자기 .env에 넣는다.
package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"tacit-qa-demo/api"
)

//go:embed web
var webFS embed.FS

// version은 빌드할 때 -ldflags "-X main.version=v1.0.0"으로 넣는다.
var version = "dev"

func main() {
	port := flag.Int("port", 0, "열 포트 번호 (0이면 비어 있는 포트를 자동으로 고름)")
	noBrowser := flag.Bool("no-browser", false, "브라우저를 자동으로 열지 않음")
	flag.Parse()

	if path, ok := loadDotEnv(); ok {
		log.Printf(".env를 읽었습니다: %s", path)
	}
	provider := api.FromEnv()

	site, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	// 127.0.0.1에만 연다. 같은 와이파이의 다른 기기에서는 접속할 수 없다.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatalf("포트를 열 수 없습니다: %v", err)
	}
	origin := "http://" + ln.Addr().String()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(site)))
	api.Register(mux, provider, origin)

	srv := &http.Server{
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	fmt.Println("==============================================")
	fmt.Printf(" 사내 Q&A 데모 %s\n", version)
	fmt.Printf(" 주소: %s/\n", origin)
	fmt.Printf(" AI 연결: %s\n", describe(provider))
	fmt.Println(" 이 창을 닫으면 데모가 종료됩니다.")
	fmt.Println("==============================================")

	if !*noBrowser {
		if err := openBrowser(origin + "/"); err != nil {
			fmt.Println(" 브라우저가 열리지 않으면 위 주소를 직접 열어 주세요.")
		}
	}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func describe(p api.Provider) string {
	if p.Name() == "none" {
		return "꺼짐 (기본값, 브라우저 안 검색만 사용)"
	}
	return fmt.Sprintf("%s (%s)", p.Name(), p.Model())
}

// securityHeaders는 브라우저가 파일 형식을 추측하거나 주소를 다른 사이트에 넘기지 않게 한다.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// loadDotEnv는 실행파일 옆, 그다음 현재 폴더의 .env를 읽는다.
// 이미 설정된 환경 변수는 덮어쓰지 않는다.
func loadDotEnv() (string, bool) {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	candidates = append(candidates, ".env")

	for _, path := range candidates {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.Trim(strings.TrimSpace(val), `"'`)
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, val)
			}
		}
		f.Close()
		return path, true
	}
	return "", false
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
