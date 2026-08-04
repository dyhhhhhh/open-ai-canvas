package service

import (
	"errors"
	"testing"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCreateTaskUsesSubmissionIDIdempotentlyPerUser(t *testing.T) {
	svc, db := newTaskStreamTestService(t)
	request := CreateTaskRequest{
		SubmissionID: "submission-1",
		Type:         "canvas_text",
		Operation:    "text",
		Prompt:       "写一段测试文案",
		Input:        map[string]any{"mode": "text"},
	}

	first, err := svc.CreateTask("user-1", request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateTask("user-1", request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent task IDs = %q, %q", first.ID, second.ID)
	}

	var count int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND submission_id = ?", "user-1", request.SubmissionID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("task count = %d, want 1", count)
	}

	otherUser, err := svc.CreateTask("user-2", request)
	if err != nil {
		t.Fatal(err)
	}
	if otherUser.ID == first.ID {
		t.Fatal("submission ID was incorrectly shared across users")
	}
}

func TestTaskTextStreamReplaysDraftAndScopesByUser(t *testing.T) {
	svc, db := newTaskStreamTestService(t)
	task := model.Task{
		ID:       "text-task-1",
		UserID:   "user-1",
		Type:     "canvas_text",
		Status:   model.TaskStatusRunning,
		Prompt:   "测试",
		Attempts: 1,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	writer, err := svc.newTaskTextWriter(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Write("第一段"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write("第二段"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "error": "上游断流"}).Error; err != nil {
		t.Fatal(err)
	}

	stream, err := svc.TaskTextStream("user-1", task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stream.Task.Status != model.TaskStatusFailed || stream.Attempt != 1 || len(stream.Chunks) != 2 {
		t.Fatalf("stream = %#v", stream)
	}
	if stream.Chunks[0].Sequence != 1 || stream.Chunks[0].Delta != "第一段" || stream.Chunks[1].Sequence != 2 || stream.Chunks[1].Delta != "第二段" {
		t.Fatalf("chunks = %#v", stream.Chunks)
	}

	afterFirst, err := svc.TaskTextStream("user-1", task.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterFirst.Chunks) != 1 || afterFirst.Chunks[0].Sequence != 2 {
		t.Fatalf("replayed chunks = %#v", afterFirst.Chunks)
	}
	if _, err := svc.TaskTextStream("user-2", task.ID, 0); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user stream error = %v, want record not found", err)
	}
}

func newTaskStreamTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir()), db
}
