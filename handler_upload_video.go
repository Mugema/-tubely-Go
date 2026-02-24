package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

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

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, 500, "Error creating temp file", err)
	}
	defer os.Remove("tubely-upload.mp4")
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, 500, "Error copying to file", err)
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

	fileName := base64.URLEncoding.EncodeToString(b) + fileType

	contentType := header.Header.Get("content-type")

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Body:        tempFile,
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
