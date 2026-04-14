package syncer

import (
	"context"
	"fmt"

	"moodletodo/internal/model"
	"moodletodo/internal/store"
	"moodletodo/internal/todoist"
)

type Engine struct {
	Store        *store.Store
	Todoist      *todoist.Client
	ProjectName  string
	UseExactDate bool
}

func (e *Engine) Sync(ctx context.Context, assignments []model.Assignment) (model.SyncResult, error) {
	result := model.SyncResult{
		Added:   []string{},
		Updated: []string{},
		Skipped: model.SkippedResult{
			Local:     []string{},
			Todoist:   []string{},
			NoChanges: []string{},
		},
		Errors: []model.SyncError{},
		Summary: model.SyncSummary{
			Total: len(assignments),
		},
	}

	projectID, err := e.Todoist.GetOrCreateProject(ctx, e.ProjectName)
	if err != nil {
		return result, err
	}

	activeTasks, err := e.Todoist.GetTasks(ctx, projectID)
	if err != nil {
		return result, err
	}
	completedTasks, err := e.Todoist.GetCompletedItems(ctx, projectID)
	if err != nil {
		return result, err
	}
	syncedMap, err := e.Store.GetSyncedTasks()
	if err != nil {
		return result, err
	}

	activeByLocalID := map[string]todoist.Task{}
	for _, task := range activeTasks {
		localID := todoist.ExtractTaskIDFromDescription(task.Description)
		if localID == "" {
			continue
		}
		activeByLocalID[localID] = task
	}

	completedByLocalID := map[string]struct{}{}
	for _, task := range completedTasks {
		localID := todoist.ExtractTaskIDFromDescription(task.Description)
		if localID == "" {
			continue
		}
		completedByLocalID[localID] = struct{}{}
	}

	type existingPair struct {
		Assignment model.Assignment
		Task       todoist.Task
	}
	newItems := make([]model.Assignment, 0)
	existingItems := make([]existingPair, 0)

	for _, assignment := range assignments {
		if assignment.Title == "" || assignment.TaskID == "" {
			continue
		}
		if assignment.IsCompleted() {
			result.Skipped.Local = append(result.Skipped.Local, assignment.Title)
			continue
		}
		if active, ok := activeByLocalID[assignment.TaskID]; ok {
			existingItems = append(existingItems, existingPair{
				Assignment: assignment,
				Task:       active,
			})
			continue
		}
		if _, ok := completedByLocalID[assignment.TaskID]; ok {
			result.Skipped.Todoist = append(result.Skipped.Todoist, assignment.Title)
			continue
		}
		if _, ok := syncedMap[assignment.TaskID]; ok {
			result.Skipped.Todoist = append(result.Skipped.Todoist, assignment.Title)
			continue
		}
		newItems = append(newItems, assignment)
	}

	for _, item := range existingItems {
		if !todoist.HasMeaningfulChanges(item.Assignment, item.Task, e.UseExactDate) {
			result.Skipped.NoChanges = append(result.Skipped.NoChanges, item.Assignment.Title)
			result.Summary.Processed++
			continue
		}
		updatedTask, updateErr := e.Todoist.UpdateTask(ctx, item.Task.ID, item.Assignment, e.UseExactDate)
		if updateErr != nil {
			result.Errors = append(result.Errors, model.SyncError{
				Title:  item.Assignment.Title,
				Reason: updateErr.Error(),
			})
			result.Summary.Failed++
			continue
		}
		if err := e.Store.MarkTaskSynced(item.Assignment.TaskID, updatedTask.ID); err != nil {
			result.Errors = append(result.Errors, model.SyncError{
				Title:  item.Assignment.Title,
				Reason: fmt.Sprintf("updated on Todoist but failed to persist sync map: %v", err),
			})
			result.Summary.Failed++
			continue
		}
		result.Updated = append(result.Updated, item.Assignment.Title)
		result.Summary.Processed++
	}

	for _, item := range newItems {
		created, createErr := e.Todoist.CreateTask(ctx, item, projectID, e.UseExactDate)
		if createErr != nil {
			result.Errors = append(result.Errors, model.SyncError{
				Title:  item.Title,
				Reason: createErr.Error(),
			})
			result.Summary.Failed++
			continue
		}
		if created.ID == "" {
			result.Errors = append(result.Errors, model.SyncError{
				Title:  item.Title,
				Reason: "Todoist API returned empty task ID",
			})
			result.Summary.Failed++
			continue
		}
		if err := e.Store.MarkTaskSynced(item.TaskID, created.ID); err != nil {
			result.Errors = append(result.Errors, model.SyncError{
				Title:  item.Title,
				Reason: fmt.Sprintf("created on Todoist but failed to persist sync map: %v", err),
			})
			result.Summary.Failed++
			continue
		}
		result.Added = append(result.Added, item.Title)
		result.Summary.Processed++
	}

	if err := e.Store.SetLastSyncNow(); err != nil {
		return result, err
	}
	return result, nil
}
