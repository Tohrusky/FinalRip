package task

import (
	"sync"
	"time"

	"github.com/TensoRaws/FinalRip/common/db"
	"github.com/TensoRaws/FinalRip/module/log"
	"github.com/TensoRaws/FinalRip/module/oss"
	"github.com/TensoRaws/FinalRip/module/resp"
	"github.com/gin-gonic/gin"
)

type ProgressRequest struct {
	VideoKey string `form:"video_key" binding:"required"`
}

type ProgressResponse struct {
	EncodeKey   string         `json:"encode_key"`
	EncodeParam string         `json:"encode_param"`
	EncodeURL   string         `json:"encode_url"`
	Key         string         `json:"key"`
	Progress    []ProgressITEM `json:"progress"`
	Script      string         `json:"script"`
	Status      string         `json:"status"`
	URL         string         `json:"url"`
}

type ProgressITEM struct {
	Completed bool   `json:"completed"`
	EncodeKey string `json:"encode_key"`
	EncodeURL string `json:"encode_url"`
	Key       string `json:"key"`
	URL       string `json:"url"`
}

// Progress 查看进度 (GET /progress)
func Progress(c *gin.Context) {
	// 绑定参数
	var req ProgressRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		resp.AbortWithMsg(c, err.Error())
		return
	}

	p, err := db.GetVideoProgress(req.VideoKey)
	if err != nil {
		log.Logger.Errorf("db.GetVideoProgress failed, err: %v", err)
		resp.AbortWithMsg(c, err.Error())
		return
	}

	// 构造每一个 clip 的信息
	progress := make([]ProgressITEM, 0)
	for _, v := range p {
		progress = append(progress, ProgressITEM{
			Completed: v.Completed,
			EncodeKey: v.EncodeKey,
			Key:       v.Key,
		})
	}
	var wg sync.WaitGroup
	for i := range progress {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// encode url
			if progress[i].EncodeKey == "" {
				progress[i].EncodeURL = ""
			} else {
				encodeUrl, err := oss.GetPresignedURL(progress[i].EncodeKey, progress[i].EncodeKey, 48*time.Hour)
				if err != nil {
					log.Logger.Errorf("oss.GetPresignedURL failed, err: %v", err)
					resp.AbortWithMsg(c, err.Error())
					return
				}
				progress[i].EncodeURL = encodeUrl
			}
			// clip url
			{
				url, err := oss.GetPresignedURL(progress[i].Key, progress[i].Key, 48*time.Hour)
				if err != nil {
					log.Logger.Errorf("oss.GetPresignedURL failed, err: %v", err)
					resp.AbortWithMsg(c, err.Error())
					return
				}
				progress[i].URL = url
			}
		}()
	}

	task, err := db.GetTask(req.VideoKey)
	if err != nil {
		log.Logger.Errorf("db.GetCompletedEncodeKey failed, err: %v", err)
		resp.AbortWithMsg(c, err.Error())
		return
	}

	var url string
	var encodeUrl string
	wg.Add(2)

	go func() {
		defer wg.Done()
		url, err = oss.GetPresignedURL(task.Key, task.Key, 48*time.Hour)
		if err != nil {
			log.Logger.Errorf("oss.GetPresignedURL failed, err: %v", err)
			resp.AbortWithMsg(c, err.Error())
			return
		}
	}()
	go func() {
		defer wg.Done()
		if task.EncodeKey == "" {
			log.Logger.Warnf("encode task not completed, key: %s", req.VideoKey)
			encodeUrl = ""
		} else {
			encodeUrl, err = oss.GetPresignedURL(task.EncodeKey, task.EncodeKey, 48*time.Hour)
			if err != nil {
				log.Logger.Errorf("oss.GetPresignedURL failed, err: %v", err)
				resp.AbortWithMsg(c, err.Error())
				return
			}
		}
	}()

	wg.Wait()

	status := "completed"
	if task.EncodeParam == "" {
		status = "pending"
	} else if task.EncodeKey == "" {
		status = "running"
	}

	resp.OKWithData(c, &ProgressResponse{
		Key:         task.Key,
		URL:         url,
		EncodeKey:   task.EncodeKey,
		EncodeParam: task.EncodeParam,
		EncodeURL:   encodeUrl,
		Progress:    progress,
		Script:      task.Script,
		Status:      status,
	})
}
