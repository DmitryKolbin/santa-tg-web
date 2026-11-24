package services

import (
	"context"
	"mime"
	"path/filepath"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
)

type FileService interface {
	Get(context.Context, Actor, string) (dto.Download, error)
}

type fileService struct {
	uow     repos.UnitOfWork
	uploads string
}

func NewFileService(uow repos.UnitOfWork, uploads string) FileService {
	return &fileService{uow: uow, uploads: uploads}
}

func (s *fileService) Get(ctx context.Context, actor Actor, key string) (dto.Download, error) {
	return execute(ctx, s.uow, actor, nil, func(repositories repos.Repositories, _ *effects) (dto.Download, error) {
		stored, err := repositories.Messages.GetFile(ctx, key)
		if err != nil {
			return dto.Download{}, err
		}
		if actor.ID != stored.Sender && actor.ID != stored.Receiver {
			return dto.Download{}, forbidden()
		}
		file := dto.Download{Key: stored.Key, Name: stored.Name, MIME: stored.Type, Path: filepath.Join(s.uploads, stored.Key)}
		if _, _, err := mime.ParseMediaType(file.MIME); err != nil {
			file.MIME = "application/octet-stream"
		}
		return file, nil
	})
}
