package service

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// windows 系统下的
var signals = []os.Signal{
	os.Interrupt, os.Kill, syscall.SIGKILL,
	syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGILL, syscall.SIGTRAP,
	syscall.SIGABRT, syscall.SIGTERM,
}

// linux 系统下的
//var signals = []os.Signal{
//	os.Interrupt, os.Kill, syscall.SIGKILL, syscall.SIGSTOP,
//	syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGILL, syscall.SIGTRAP,
//	syscall.SIGABRT, syscall.SIGSYS, syscall.SIGTERM,
//}

// App 代表应用本身
type App struct {
	servers []*Server

	// 优雅退出整个超时时间，默认30秒
	shutdownTimeout time.Duration

	// 优雅退出时候等待处理已有请求时间，默认10秒钟
	waitTime time.Duration

	// 自定义回调超时时间，默认三秒钟
	cbTimeout time.Duration

	cbs []ShutdownCallback
}

// Option option模式
type Option func(*App)

// ShutdownCallback 采用 context.Context 来控制超时，而不是用 time.After 是因为
// - 超时本质上是使用这个回调的人控制的
// - 我们还希望用户知道，他的回调必须要在一定时间内处理完毕，而且他必须显式处理超时错误
type ShutdownCallback func(ctx context.Context)

func WithShutdownCallbacks(cbs ...ShutdownCallback) Option {
	return func(app *App) {
		app.cbs = cbs
	}
}

func WithTimeCfg(shutdownTimeout, waitTime, cbTimeout time.Duration) Option {
	return func(app *App) {
		app.shutdownTimeout = shutdownTimeout
		app.waitTime = waitTime
		app.cbTimeout = cbTimeout
	}
}

func NewApp(servers []*Server, opts ...Option) *App {
	app := &App{
		servers: servers,
	}
	for _, opt := range opts {
		opt(app)
	}

	return app
}

// Run 应用运行
func (app *App) Run() {
	// 应用运行，需要把其包含的所有服务起来
	for _, s := range app.servers {
		srv := s
		go func() {
			if err := srv.start(); err != nil {
				if err == http.ErrServerClosed {
					log.Printf("服务%s已关闭", srv.name)
				} else {
					log.Printf("服务%s异常: %v", srv.name, err)
				}
			}
		}()
	}

	// 监听退出信号
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, signals...)

	timeout := time.After(2 * time.Second)

	select {
	case <-ch:
		log.Println("强制退出")
	case <-timeout:
		log.Println("超时退出")
	}

	app.shutdown()
}

func (app *App) shutdown() {
	log.Println("开始关闭应用，停止接受新请求")
	for _, s := range app.servers {
		s.rejectReq()
	}

	log.Println("等待正在执行请求完结")
	time.Sleep(app.waitTime)

	log.Println("停止所有服务")
	var wg sync.WaitGroup
	wg.Add(len(app.servers))
	for _, s := range app.servers {
		srv := s
		go func() {
			defer wg.Done()
			if err := srv.stop(); err != nil {
				log.Printf("关闭服务%s失败:%v\n", srv.name, err)
			}
		}()
	}
	wg.Wait()

	log.Println("开始执行自定义回调")
	wg.Add(len(app.cbs))
	for _, cb := range app.cbs {
		c := cb
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), app.cbTimeout)
			c(ctx)
			cancel()
		}()
	}
	wg.Wait()

	log.Println("开始释放资源")
	app.close()
}

func (app *App) close() {
	time.Sleep(time.Second)
	log.Println("应用关闭")
}

// Server 代表服务
type Server struct {
	name string
	srv  *http.Server
	mux  *serverMux
}

func NewServer(name string, addr string) *Server {
	mux := &serverMux{
		ServeMux: http.NewServeMux(),
		reject:   false,
	}
	return &Server{
		name: name,
		mux:  mux,
		srv: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

func (s *Server) rejectReq() {
	s.mux.reject = true
}

func (s *Server) start() error {
	return s.srv.ListenAndServe()
}

func (s *Server) stop() error {
	log.Printf("服务%s关闭中\n", s.name)
	return s.srv.Shutdown(context.Background())
}

// serverMux 既可以看做是装饰器模式，也可以看做委托模式
// 拒绝新请求：封装serverMux，在每一次处理请求之前，先检查一下当前是否需要拒绝新请求，reject标志位
type serverMux struct {
	*http.ServeMux
	reject bool
}

func (s *serverMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 拒绝请求
	if s.reject {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("服务已关闭"))
		return
	}
	// 服务正常运行，则正常处理请求
	s.ServeMux.ServeHTTP(w, r)
}
