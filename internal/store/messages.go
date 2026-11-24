package store

import "context"

func (s *Store) Thread(ctx context.Context, key string) (ThreadView, error) {
	var t ThreadView
	err := s.db.NewSelect().TableExpr("assignments AS a").
		ColumnExpr("a.thread,a.game,g.name AS game_name,a.round,a.sender,a.receiver,g.round AS current_round,g.started").
		Join("JOIN games AS g ON g.id=a.game").Where("a.thread=?", key).Scan(ctx, &t)
	return t, err
}

// Threads lists every conversation of user in game, newest round first.
func (s *Store) Threads(ctx context.Context, game, user int64) ([]ThreadView, error) {
	var result []ThreadView
	err := s.db.NewSelect().TableExpr("assignments AS a").
		ColumnExpr("a.thread,a.game,a.round,a.sender,a.receiver,u.name AS receiver_name").
		ColumnExpr("COALESCE(d.tracking_number,'') AS tracking_number,COALESCE(d.received,0) AS received").
		ColumnExpr("(SELECT count(*) FROM messages m WHERE m.thread=a.thread AND m.receiver=? AND m.id>COALESCE((SELECT message FROM message_reads r WHERE r.person=? AND r.game=a.game AND r.thread=a.thread),0)) AS unread_count", user, user).
		Join("JOIN users AS u ON u.id=a.receiver").Join("LEFT JOIN deliveries AS d ON d.thread=a.thread").
		Where("a.game=? AND (a.sender=? OR a.receiver=?)", game, user, user).
		OrderExpr("a.round DESC,a.thread").Scan(ctx, &result)
	return result, err
}

// Messages returns up to 100 messages after the cursor; an empty thread means
// game announcements.
func (s *Store) Messages(ctx context.Context, user, game int64, thread string, after int64) ([]MessageView, error) {
	var result []MessageView
	query := s.db.NewSelect().TableExpr("messages AS m").
		ColumnExpr("m.id,m.text,m.sender=? AS mine,m.file_key,m.file_name,m.file_type,m.created", user).
		Where("m.game=? AND m.id>?", game, after).OrderExpr("m.id").Limit(100)
	if thread == "" {
		query.Where("m.thread IS NULL")
	} else {
		query.Where("m.thread=?", thread)
	}
	err := query.Scan(ctx, &result)
	return result, err
}

func (s *Store) MarkRead(ctx context.Context, user, game int64, thread string, message int64) error {
	read := &MessageRead{PersonID: user, GameID: game, Thread: thread, MessageID: message}
	_, err := s.db.NewInsert().Model(read).On("CONFLICT (person,game,thread) DO UPDATE").
		Set("message=max(message,EXCLUDED.message)").Exec(ctx)
	return err
}

func (s *Store) LatestAnnouncement(ctx context.Context, user, game int64) (MessageView, error) {
	var result MessageView
	err := s.db.NewSelect().TableExpr("messages AS m").
		ColumnExpr("m.id,m.text,m.sender=? AS mine,m.file_key,m.file_name,m.file_type,m.created", user).
		Where("m.game=? AND m.thread IS NULL", game).OrderExpr("m.id DESC").Limit(1).Scan(ctx, &result)
	return result, err
}

func (s *Store) UnreadAnnouncements(ctx context.Context, user, game int64) (int, error) {
	var count int
	err := s.db.NewSelect().TableExpr("messages AS m").ColumnExpr("count(*)").
		Where("m.game=? AND m.thread IS NULL AND m.sender<>?", game, user).
		Where("m.id>COALESCE((SELECT message FROM message_reads WHERE person=? AND game=? AND thread=''),0)", user, game).
		Scan(ctx, &count)
	return count, err
}

func (s *Store) CreateMessage(ctx context.Context, message *Message) error {
	_, err := s.db.NewInsert().Model(message).ExcludeColumn("id", "created").Exec(ctx)
	return err
}

func (s *Store) Delivery(ctx context.Context, thread string) (Delivery, error) {
	var result Delivery
	err := s.db.NewSelect().Model(&result).Where("d.thread=?", thread).Scan(ctx)
	return result, err
}

// SaveDelivery upserts the delivery and overwrites only the given columns of
// an existing row.
func (s *Store) SaveDelivery(ctx context.Context, delivery *Delivery, columns ...string) error {
	query := s.db.NewInsert().Model(delivery).On("CONFLICT (thread) DO UPDATE")
	for _, column := range columns {
		query.Set(column + "=EXCLUDED." + column)
	}
	_, err := query.Exec(ctx)
	return err
}

func (s *Store) File(ctx context.Context, key string) (File, error) {
	var f File
	err := s.db.NewSelect().TableExpr("messages").
		ColumnExpr("file_key AS key,file_name AS name,file_type AS type,sender,receiver").
		Where("file_key=? AND file_key<>''", key).Scan(ctx, &f)
	return f, err
}
