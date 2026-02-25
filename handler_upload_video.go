package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	const maxMemory = 1 << 30
	videoIdString := r.PathValue("videoID")

	if videoIdString == "" {
		respondWithError(w, http.StatusBadRequest, "No videoID provided", errors.New("id missing"))
		return

	}

	videoID, err := uuid.Parse(videoIdString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error with the id provided", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Not valid user", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Video not found", err)
		return
	}

	if userID != video.UserID {
		respondWithError(w, http.StatusUnauthorized, "not video owner", err)
		return
	}

	err = r.ParseMultipartForm(maxMemory)
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "form file error", err)
		return
	}
	defer file.Close()

	fileType, _, err := mime.ParseMediaType(header.Header.Get("content-type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error wrong file type", err)
		return
	}

	fileType = getExtension(fileType)
	if fileType != "mp4" {
		respondWithError(w, http.StatusBadRequest, "Error wrong file type", errors.New("wrong file"))
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload-*.mp4")
	if err != nil {
		respondWithError(w, 500, "Error creating temp file", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, 500, "Error copying to file", err)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		return
	}

	b := make([]byte, 32)
	_, err = rand.Read(b)
	if err != nil {
		respondWithError(w, 500, "Error creating new filename", err)
	}

	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, 500, "Error getting the aspect ratio", err)
		return
	}

	var aspectType string
	if aspectRatio == "16:9" {
		aspectType = "landscape"
	} else if aspectRatio == "9:16" {
		aspectType = "portrait"
	} else {
		aspectType = "other"
	}

	processedVideo, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		fmt.Printf("Error %v\n", err)
		respondWithError(w, 500, "Error processing the file", err)
		return
	}

	videoFile, err := os.Open(processedVideo)
	if err != nil {
		respondWithError(w, 500, "Error opening the file", err)
		return
	}
	defer videoFile.Close()

	fileName := fmt.Sprintf("%v/%v.%v", aspectType, base64.URLEncoding.EncodeToString(b), fileType)

	contentType := header.Header.Get("content-type")

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Body:        videoFile,
		Key:         &fileName,
		ContentType: &contentType,
	})
	if err != nil {
		respondWithError(w, 500, "error uploading the file", err)
	}

	newVideoUrl := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, fileName)

	video.VideoURL = &newVideoUrl

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, 501, "Database error", err)
	}

	w.Header().Set("content-type", contentType)
	respondWithJSON(w, http.StatusOK, video)
	return
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_streams", filePath)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()

	if err != nil {
		fmt.Printf("error is at 1 %v\n", err)
		return "", err
	}

	type result struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	ffprobeResult := result{}

	err = json.Unmarshal(stdout.Bytes(), &ffprobeResult)
	if err != nil {
		fmt.Printf("error is at 2 %v\n", err)
		return "", err
	}

	if ffprobeResult.Streams[0].Width == 16*ffprobeResult.Streams[0].Height/9 {
		return "16:9", nil
	} else if ffprobeResult.Streams[0].Height == 16*ffprobeResult.Streams[0].Width/9 {
		return "9:16", nil
	}
	return "other", nil
}

func processVideoForFastStart(filepath string) (string, error) {
	fileName := filepath + ".processing"

	cmd := exec.Command(
		"ffmpeg",
		"-i", filepath,
		"-c", "copy",
		"-movflags", "faststart",
		"-f", "mp4", fileName)

	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return fileName, nil
}
