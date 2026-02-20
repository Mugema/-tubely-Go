package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, 501, "Unable to assign memory", err)
	}
	mimeType, _, _ := mime.ParseMediaType(r.Header.Get("content-type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error", err)
	}
	if mimeType != "image/jpeg" && mimeType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "invalid file type", errors.New("wrong file"))
	}

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, 501, "Error form file", err)
	}
	mediaType := header.Header.Get("content-type")

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, 501, "Database error", err)
	}

	if video.UserID != userID {
		respondWithError(w, 501, "Database error", err)
	}

	fileName := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%v.%v", videoID, "png"))

	thumbNailFile, err := os.Create(fileName)
	if err != nil {
		respondWithError(w, 501, "Error creating file", err)
	}
	defer file.Close()

	_, err = io.Copy(thumbNailFile, file)
	if err != nil {
		respondWithError(w, 501, "Error writing to file", err)
	}

	url := fmt.Sprintf("http://localhost:<%v>/assets/<%v>.<%v>", cfg.port, videoID, mediaType)
	video.ThumbnailURL = &url

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, 501, "Database error", err)
	}

	respondWithJSON(w, http.StatusOK, video)

}
