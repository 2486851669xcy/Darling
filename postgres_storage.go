package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lib/pq"
)

type postgresScanner interface {
	Scan(dest ...any) error
}

func (a *App) postgresUserID(ctx context.Context) (string, error) {
	if a == nil || a.db == nil {
		return "", errors.New("PostgreSQL database is unavailable")
	}
	session, err := userSessionFromContext(ctx)
	if err != nil {
		return "", err
	}
	userID, err := normalizePostgresUserUUID(session.ID)
	if err != nil {
		return "", fmt.Errorf("read PostgreSQL user from context: %w", err)
	}
	return userID, nil
}

func (a *App) clearMessages(ctx context.Context, characterID string) error {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
DELETE FROM messages
WHERE user_id = $1::uuid AND character_id = $2`, userID, characterID)
	return err
}

func (a *App) getStickerCadence(ctx context.Context, characterID string, limit int) StickerCadence {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		log.Printf("query sticker cadence failed: %v", err)
		return StickerCadence{MessagesSinceLast: -1}
	}
	rows, err := a.db.QueryContext(ctx, `
SELECT message_type
FROM messages
WHERE user_id = $1::uuid AND character_id = $2
ORDER BY id DESC
LIMIT $3`, userID, characterID, limit)
	if err != nil {
		log.Printf("query sticker cadence failed: %v", err)
		return StickerCadence{MessagesSinceLast: -1}
	}
	defer rows.Close()

	cadence := StickerCadence{MessagesSinceLast: -1}
	index := 0
	for rows.Next() {
		var messageType string
		if err := rows.Scan(&messageType); err != nil {
			log.Printf("scan sticker cadence failed: %v", err)
			return cadence
		}
		if messageType == "sticker" {
			cadence.RecentCount++
			if cadence.MessagesSinceLast < 0 {
				cadence.MessagesSinceLast = index
			}
		}
		index++
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate sticker cadence failed: %v", err)
	}
	return cadence
}

func (a *App) getRecentStickerSet(ctx context.Context, characterID string, limit int) map[string]bool {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		log.Printf("query recent stickers failed: %v", err)
		return nil
	}
	rows, err := a.db.QueryContext(ctx, `
SELECT content
FROM messages
WHERE user_id = $1::uuid
  AND character_id = $2
  AND message_type = 'sticker'
ORDER BY id DESC
LIMIT $3`, userID, characterID, limit)
	if err != nil {
		log.Printf("query recent stickers failed: %v", err)
		return nil
	}
	defer rows.Close()

	recent := make(map[string]bool)
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			log.Printf("scan recent sticker failed: %v", err)
			return recent
		}
		recent[content] = true
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate recent stickers failed: %v", err)
	}
	return recent
}

func (a *App) canSendProactiveMessage(ctx context.Context, characterID string) (bool, int, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return false, 60, err
	}
	var sender string
	var createdAt time.Time
	err = a.db.QueryRowContext(ctx, `
SELECT sender, created_at
FROM messages
WHERE user_id = $1::uuid AND character_id = $2
ORDER BY id DESC
LIMIT 1`, userID, characterID).Scan(&sender, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, 0, nil
		}
		return false, 60, err
	}

	ageSeconds := int64(time.Since(createdAt).Seconds())
	if ageSeconds < 90 {
		return false, int(90 - ageSeconds), nil
	}
	if sender == "character" && ageSeconds < 360 {
		return false, int(360 - ageSeconds), nil
	}
	return true, 0, nil
}

func (a *App) hasMomentCheck(ctx context.Context, momentID int64, author string) bool {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		log.Printf("check moment check failed: %v", err)
		return false
	}
	var exists bool
	err = a.db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM moment_checks
  WHERE user_id = $1::uuid
    AND moment_id = $2
    AND author = $3
)`, userID, momentID, author).Scan(&exists)
	if err != nil {
		log.Printf("check moment check failed: %v", err)
		return false
	}
	return exists
}

func (a *App) saveMomentCheck(ctx context.Context, momentID int64, author, action string) error {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return err
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "seen"
	}
	_, err = a.db.ExecContext(ctx, `
INSERT INTO moment_checks(user_id, moment_id, author, action)
VALUES ($1::uuid, $2, $3, $4)
ON CONFLICT (user_id, moment_id, author)
DO UPDATE SET action = EXCLUDED.action`, userID, momentID, author, action)
	return err
}

func (a *App) getGeneratedStickerCandidates(ctx context.Context, characterID, emotion string) []string {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		log.Printf("query sticker assets failed: %v", err)
		return nil
	}
	rows, err := a.db.QueryContext(ctx, `
SELECT url
FROM sticker_assets
WHERE user_id = $1::uuid
  AND character_id = $2
  AND emotion = $3
ORDER BY COALESCE(last_used_at, created_at) DESC, id DESC
LIMIT 24`, userID, characterID, emotion)
	if err != nil {
		log.Printf("query sticker assets failed: %v", err)
		return nil
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			log.Printf("scan sticker asset failed: %v", err)
			return items
		}
		items = append(items, url)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate sticker assets failed: %v", err)
	}
	return items
}

func (a *App) countGeneratedStickerAssets(ctx context.Context, characterID, emotion string) int {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		log.Printf("count sticker assets failed: %v", err)
		return 0
	}
	var count int
	err = a.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sticker_assets
WHERE user_id = $1::uuid
  AND character_id = $2
  AND emotion = $3`, userID, characterID, emotion).Scan(&count)
	if err != nil {
		log.Printf("count sticker assets failed: %v", err)
		return 0
	}
	return count
}

func (a *App) touchStickerAsset(ctx context.Context, characterID, emotion, url string) error {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
UPDATE sticker_assets
SET last_used_at = CURRENT_TIMESTAMP
WHERE user_id = $1::uuid
  AND character_id = $2
  AND emotion = $3
  AND url = $4`, userID, characterID, emotion, url)
	return err
}

func (a *App) hasStickerAsset(ctx context.Context, characterID, emotion, url string) bool {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		log.Printf("check sticker asset failed: %v", err)
		return false
	}
	var exists bool
	err = a.db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM sticker_assets
  WHERE user_id = $1::uuid
    AND character_id = $2
    AND emotion = $3
    AND url = $4
)`, userID, characterID, emotion, url).Scan(&exists)
	if err != nil {
		log.Printf("check sticker asset failed: %v", err)
		return false
	}
	return exists
}

func (a *App) saveStickerAsset(ctx context.Context, characterID, emotion, url, source, prompt string) error {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
INSERT INTO sticker_assets(
  user_id, character_id, emotion, url, source, prompt, last_used_at
)
SELECT $1::uuid, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP
WHERE NOT EXISTS (
  SELECT 1
  FROM sticker_assets
  WHERE user_id = $1::uuid
    AND character_id = $2
    AND emotion = $3
    AND url = $4
)`, userID, characterID, emotion, url, source, prompt)
	return err
}

func (a *App) saveMessage(ctx context.Context, characterID, sender, messageType, content string) (Message, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return Message{}, err
	}
	return scanPostgresMessage(a.db.QueryRowContext(ctx, `
INSERT INTO messages(user_id, character_id, sender, message_type, content)
VALUES ($1::uuid, $2, $3, $4, $5)
RETURNING id, character_id, sender, message_type, content, created_at`,
		userID, characterID, sender, messageType, content,
	))
}

func (a *App) getMessageByID(ctx context.Context, id int64) (Message, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return Message{}, err
	}
	return scanPostgresMessage(a.db.QueryRowContext(ctx, `
SELECT id, character_id, sender, message_type, content, created_at
FROM messages
WHERE user_id = $1::uuid AND id = $2`, userID, id))
}

func (a *App) getMessages(ctx context.Context, characterID string, limit int) ([]Message, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `
SELECT id, character_id, sender, message_type, content, created_at
FROM messages
WHERE user_id = $1::uuid AND character_id = $2
ORDER BY id ASC
LIMIT $3`, userID, characterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPostgresMessages(rows)
}

func (a *App) getRecentMessages(ctx context.Context, characterID string, limit int) ([]Message, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `
SELECT id, character_id, sender, message_type, content, created_at
FROM (
  SELECT id, character_id, sender, message_type, content, created_at
  FROM messages
  WHERE user_id = $1::uuid AND character_id = $2
  ORDER BY id DESC
  LIMIT $3
) recent_messages
ORDER BY id ASC`, userID, characterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPostgresMessages(rows)
}

func scanPostgresMessages(rows *sql.Rows) ([]Message, error) {
	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanPostgresMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func scanPostgresMessage(scanner postgresScanner) (Message, error) {
	var message Message
	var createdAt time.Time
	err := scanner.Scan(
		&message.ID,
		&message.CharacterID,
		&message.Sender,
		&message.Type,
		&message.Content,
		&createdAt,
	)
	if err != nil {
		return Message{}, err
	}
	message.CreatedAt = formatPostgresTimestamp(createdAt)
	return message, nil
}

func (a *App) getMoments(ctx context.Context, characterID string, limit int) ([]Moment, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `
SELECT id, character_id, author, content, image_url, created_at
FROM moments
WHERE user_id = $1::uuid AND character_id = $2
ORDER BY id DESC
LIMIT $3`, userID, characterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	moments := make([]Moment, 0)
	momentIDs := make([]int64, 0)
	for rows.Next() {
		moment, err := scanPostgresMoment(rows)
		if err != nil {
			return nil, err
		}
		moments = append(moments, moment)
		momentIDs = append(momentIDs, moment.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(momentIDs) == 0 {
		return moments, nil
	}

	comments, err := a.getMomentComments(ctx, momentIDs)
	if err != nil {
		return nil, err
	}
	likes, err := a.getMomentLikes(ctx, momentIDs)
	if err != nil {
		return nil, err
	}
	for index := range moments {
		moments[index].Likes = likes[moments[index].ID]
		moments[index].Comments = comments[moments[index].ID]
	}
	return moments, nil
}

func (a *App) getMomentLikes(ctx context.Context, momentIDs []int64) (map[int64][]MomentLike, error) {
	likes := make(map[int64][]MomentLike)
	if len(momentIDs) == 0 {
		return likes, nil
	}
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `
SELECT id, moment_id, author, created_at
FROM moment_likes
WHERE user_id = $1::uuid
  AND moment_id = ANY($2::bigint[])
ORDER BY id ASC`, userID, pq.Array(momentIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		like, err := scanPostgresMomentLike(rows)
		if err != nil {
			return nil, err
		}
		likes[like.MomentID] = append(likes[like.MomentID], like)
	}
	return likes, rows.Err()
}

func (a *App) getMomentComments(ctx context.Context, momentIDs []int64) (map[int64][]MomentComment, error) {
	comments := make(map[int64][]MomentComment)
	if len(momentIDs) == 0 {
		return comments, nil
	}
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `
SELECT id, moment_id, author, content, created_at
FROM moment_comments
WHERE user_id = $1::uuid
  AND moment_id = ANY($2::bigint[])
ORDER BY id ASC`, userID, pq.Array(momentIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		comment, err := scanPostgresMomentComment(rows)
		if err != nil {
			return nil, err
		}
		comments[comment.MomentID] = append(comments[comment.MomentID], comment)
	}
	return comments, rows.Err()
}

func (a *App) saveMoment(ctx context.Context, characterID, author, content, imageURL string) (Moment, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return Moment{}, err
	}
	return scanPostgresMoment(a.db.QueryRowContext(ctx, `
INSERT INTO moments(user_id, character_id, author, content, image_url)
VALUES ($1::uuid, $2, $3, $4, $5)
RETURNING id, character_id, author, content, image_url, created_at`,
		userID, characterID, author, content, imageURL,
	))
}

func (a *App) saveMomentLike(ctx context.Context, momentID int64, author string) (MomentLike, bool, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return MomentLike{}, false, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return MomentLike{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
INSERT INTO moment_likes(user_id, moment_id, author)
VALUES ($1::uuid, $2, $3)
ON CONFLICT (user_id, moment_id, author) DO NOTHING`, userID, momentID, author)
	if err != nil {
		return MomentLike{}, false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return MomentLike{}, false, err
	}
	like, err := scanPostgresMomentLike(tx.QueryRowContext(ctx, `
SELECT id, moment_id, author, created_at
FROM moment_likes
WHERE user_id = $1::uuid
  AND moment_id = $2
  AND author = $3`, userID, momentID, author))
	if err != nil {
		return MomentLike{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return MomentLike{}, false, err
	}
	return like, rowsAffected > 0, nil
}

func (a *App) saveMomentComment(ctx context.Context, momentID int64, author, content string) (MomentComment, error) {
	userID, err := a.postgresUserID(ctx)
	if err != nil {
		return MomentComment{}, err
	}
	return scanPostgresMomentComment(a.db.QueryRowContext(ctx, `
INSERT INTO moment_comments(user_id, moment_id, author, content)
VALUES ($1::uuid, $2, $3, $4)
RETURNING id, moment_id, author, content, created_at`,
		userID, momentID, author, content,
	))
}

func scanPostgresMoment(scanner postgresScanner) (Moment, error) {
	var moment Moment
	var createdAt time.Time
	err := scanner.Scan(
		&moment.ID,
		&moment.CharacterID,
		&moment.Author,
		&moment.Content,
		&moment.ImageURL,
		&createdAt,
	)
	if err != nil {
		return Moment{}, err
	}
	moment.CreatedAt = formatPostgresTimestamp(createdAt)
	return moment, nil
}

func scanPostgresMomentLike(scanner postgresScanner) (MomentLike, error) {
	var like MomentLike
	var createdAt time.Time
	err := scanner.Scan(&like.ID, &like.MomentID, &like.Author, &createdAt)
	if err != nil {
		return MomentLike{}, err
	}
	like.CreatedAt = formatPostgresTimestamp(createdAt)
	return like, nil
}

func scanPostgresMomentComment(scanner postgresScanner) (MomentComment, error) {
	var comment MomentComment
	var createdAt time.Time
	err := scanner.Scan(
		&comment.ID,
		&comment.MomentID,
		&comment.Author,
		&comment.Content,
		&createdAt,
	)
	if err != nil {
		return MomentComment{}, err
	}
	comment.CreatedAt = formatPostgresTimestamp(createdAt)
	return comment, nil
}

func formatPostgresTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
