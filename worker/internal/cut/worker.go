package cut

import (
	"context"
	"os"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/TensoRaws/FinalRip/common/db"
	"github.com/TensoRaws/FinalRip/common/task"
	"github.com/TensoRaws/FinalRip/module/ffmpeg"
	"github.com/TensoRaws/FinalRip/module/log"
	"github.com/TensoRaws/FinalRip/module/oss"
	"github.com/TensoRaws/FinalRip/module/queue"
	"github.com/TensoRaws/FinalRip/module/util"
	"github.com/bytedance/sonic"
	"github.com/hibiken/asynq"
)

// Start starts the worker
func Start() {
	mux := asynq.NewServeMux()
	mux.HandleFunc(task.VIDEO_CUT, Handler)

	if err := queue.Qs.Run(mux); err != nil {
		log.Logger.Fatalf("could not start worker: %v", err)
	}
}

func Handler(ctx context.Context, t *asynq.Task) error {
	var p task.CutTaskPayload
	if err := sonic.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	log.Logger.Infof("Processing task CUT with payload %v", p.VideoKey)

	tempPath := "_temp"
	tempVideo := tempPath + path.Ext(p.VideoKey)

	// 清理临时文件
	_ = util.ClaerTempFile(tempPath, tempVideo)
	defer func(p ...string) {
		log.Logger.Infof("Clear temp file %v", p)
		_ = util.ClaerTempFile(p...)
	}(tempPath, tempVideo)

	_ = os.Mkdir(tempPath, os.ModePerm)

	err := oss.GetWithPath(p.VideoKey, tempVideo)
	if err != nil {
		log.Logger.Errorf("Failed to download video %s: %v", p.VideoKey, err)
		return err
	}

	// 等待下载完成
	log.Logger.Infof("Waiting for video %s to download", p.VideoKey)
	for {
		if _, err := os.Stat(tempVideo); err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	log.Logger.Infof("Video %s downloaded", p.VideoKey)

	outputs, err := ffmpeg.CutVideo(tempVideo, tempPath)
	if err != nil {
		log.Logger.Errorf("Failed to cut video %s: %v", p.VideoKey, err)
		return err
	}

	// 上传切片
	var wg sync.WaitGroup
	total := len(outputs)
	for i, output := range outputs {
		wg.Add(1)

		go func(index int, file string) {
			defer wg.Done()

			key := p.VideoKey + "-clip-" + strconv.FormatInt(int64(index), 10) + ".mkv"

			// 正常情况
			if db.CheckVideoExist(db.VideoClipInfo{
				Key:     p.VideoKey,
				ClipKey: key,
			}) {
				log.Logger.Infof("Video Clip %s already exists", key)
				return
			}

			err := oss.PutByPath(key, file)
			if err != nil {
				log.Logger.Errorf("Failed to upload video %s: %s", key, file)
			}

			err = db.InsertVideo(db.VideoClipInfo{
				Key:     p.VideoKey,
				Index:   index,
				Total:   total,
				ClipKey: key,
			})
			if err != nil {
				log.Logger.Errorf("Failed to insert video %s: %v", key, err)
			}
		}(i, output)
	}
	wg.Wait()

	return nil
}
