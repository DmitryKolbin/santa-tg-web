package repos

import (
	"context"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/projections"
	"github.com/uptrace/bun"
)

type MessageRepo interface {
	GetThread(context.Context, string) (projections.ThreadDetails, error)
	ListThreads(context.Context, int64, int64) ([]projections.ThreadDetails, error)
	List(context.Context, int64, int64, *string, int64) ([]projections.MessageDetails, error)
	MarkRead(context.Context, int64, int64, string, int64) error
	LatestAnnouncement(context.Context, int64, int64) (projections.MessageDetails, error)
	UnreadAnnouncements(context.Context, int64, int64) (int, error)
	Create(context.Context, *santa.Message) error
	GetDelivery(context.Context, string) (santa.Delivery, error)
	SaveDelivery(context.Context, *santa.Delivery, ...string) error
	GetFile(context.Context, string) (projections.StoredFile, error)
}

type messageRepo struct{ db bun.IDB }

func NewMessageRepo(db bun.IDB) MessageRepo { return &messageRepo{db: db} }

func (r *messageRepo) GetThread(ctx context.Context, key string) (projections.ThreadDetails, error) {
	var t projections.ThreadDetails
	err := r.db.NewSelect().TableExpr("assignments AS a").
		ColumnExpr("a.thread,a.game,g.name AS game_name,a.round,a.sender,a.receiver,g.round AS current_round,g.started").
		Join("JOIN games AS g ON g.id=a.game").Where("a.thread=?", key).Scan(ctx, &t)
	return t, err
}

func (r *messageRepo) ListThreads(ctx context.Context, game, user int64) ([]projections.ThreadDetails, error) {
	var result []projections.ThreadDetails
	err := r.db.NewSelect().TableExpr("assignments AS a").
		ColumnExpr("a.thread,a.game,a.round,a.sender,a.receiver,u.name AS receiver_name").
		ColumnExpr("COALESCE(d.tracking_number,'') AS tracking_number,COALESCE(d.received,0) AS received").
		ColumnExpr("(SELECT count(*) FROM messages m WHERE m.thread=a.thread AND m.receiver=? AND m.id>COALESCE((SELECT message FROM message_reads r WHERE r.person=? AND r.game=a.game AND r.thread=a.thread),0)) AS unread_count", user, user).
		Join("JOIN users AS u ON u.id=a.receiver").Join("LEFT JOIN deliveries AS d ON d.thread=a.thread").
		Where("a.game=? AND (a.sender=? OR a.receiver=?)", game, user, user).
		OrderExpr("a.round DESC,a.thread").Scan(ctx, &result)
	return result, err
}

func (r *messageRepo) List(ctx context.Context, user, game int64, thread *string, after int64) ([]projections.MessageDetails, error) {
	var result []projections.MessageDetails
	query := r.db.NewSelect().TableExpr("messages AS m").
		ColumnExpr("m.id,m.text,m.sender=? AS mine,m.file_key,m.file_name,m.file_type,m.created", user).
		Where("m.game=? AND m.id>?", game, after).OrderExpr("m.id").Limit(100)
	if thread == nil {
		query.Where("m.thread IS NULL")
	} else {
		query.Where("m.thread=?", *thread)
	}
	err := query.Scan(ctx, &result)
	return result, err
}

func (r *messageRepo) MarkRead(ctx context.Context, user, game int64, thread string, message int64) error {
	read := &santa.MessageRead{PersonID: user, GameID: game, Thread: thread, MessageID: message}
	_, err := r.db.NewInsert().Model(read).On("CONFLICT (person,game,thread) DO UPDATE").
		Set("message=max(message,EXCLUDED.message)").Exec(ctx)
	return err
}

func (r *messageRepo) LatestAnnouncement(ctx context.Context, user, game int64) (projections.MessageDetails, error) {
	var result projections.MessageDetails
	err := r.db.NewSelect().TableExpr("messages AS m").
		ColumnExpr("m.id,m.text,m.sender=? AS mine,m.file_key,m.file_name,m.file_type,m.created", user).
		Where("m.game=? AND m.thread IS NULL", game).OrderExpr("m.id DESC").Limit(1).Scan(ctx, &result)
	return result, err
}

func (r *messageRepo) UnreadAnnouncements(ctx context.Context, user, game int64) (int, error) {
	var count int
	err := r.db.NewSelect().TableExpr("messages AS m").ColumnExpr("count(*)").
		Where("m.game=? AND m.thread IS NULL AND m.sender<>?", game, user).
		Where("m.id>COALESCE((SELECT message FROM message_reads WHERE person=? AND game=? AND thread=''),0)", user, game).
		Scan(ctx, &count)
	return count, err
}

func (r *messageRepo) Create(ctx context.Context, message *santa.Message) error {
	_, err := r.db.NewInsert().Model(message).ExcludeColumn("id", "created").Exec(ctx)
	return err
}

func (r *messageRepo) GetDelivery(ctx context.Context, thread string) (santa.Delivery, error) {
	var result santa.Delivery
	err := r.db.NewSelect().Model(&result).Where("d.thread=?", thread).Scan(ctx)
	return result, err
}

func (r *messageRepo) SaveDelivery(ctx context.Context, delivery *santa.Delivery, columns ...string) error {
	query := r.db.NewInsert().Model(delivery).On("CONFLICT (thread) DO UPDATE")
	for _, column := range columns {
		query.Set(column + "=EXCLUDED." + column)
	}
	_, err := query.Exec(ctx)
	return err
}

func (r *messageRepo) GetFile(ctx context.Context, key string) (projections.StoredFile, error) {
	var f projections.StoredFile
	err := r.db.NewSelect().TableExpr("messages").
		ColumnExpr("file_key AS key,file_name AS name,file_type AS type,sender,receiver").
		Where("file_key=? AND file_key<>''", key).Scan(ctx, &f)
	return f, err
}
