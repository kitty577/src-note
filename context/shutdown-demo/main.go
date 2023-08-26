package main

import (
	"context"
	"log"
	"net/http"
	"shutdown-demo/service"
	"time"
)

func main() {
	s1 := service.NewServer("business", "localhost:8080")
	s1.Handle("/business/shutdown", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello world!"))
	}))
	s2 := service.NewServer("admin", "localhost:8081")
	app := service.NewApp([]*service.Server{s1, s2}, service.WithShutdownCallbacks(StoreCacheToDBCallBack), service.WithTimeCfg(30*time.Second, 10*time.Second, 3*time.Second))
	app.Run()
}

// 在关闭应用时，回调处理一些业务：比如缓存刷新到数据库中

func StoreCacheToDBCallBack(ctx context.Context) {
	done := make(chan struct{}, 1)
	go func() {
		log.Println("刷新缓存中...")
		time.Sleep(2 * time.Second)
		done <- struct{}{}
	}()
	select {
	case <-ctx.Done():
		log.Println("ctx超时")
	case <-done:
		log.Println("刷新缓存完成")
	}
	log.Println("回调处理完成")
}
