package task

import (
	"context"
	"fmt"
	"net/url"
	"os"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
)

type Task struct {
	method    cloudtaskspb.HttpMethod
	url       string
	values    url.Values
	queueName string
}

func NewPostTask(url string, values url.Values, queueName string) *Task {
	return &Task{cloudtaskspb.HttpMethod_POST, url, values, queueName}
}

type Queue interface {
	SubmitTask(ctx context.Context, task *Task) error
	Close() error
}

type cloudTaskQueue struct {
	client      *cloudtasks.Client
	queuePrefix string
}

func NewCloudTaskQueue(ctx context.Context) (Queue, error) {
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	prefix := fmt.Sprintf("projects/%s/locations/%s/queues/", os.Getenv("GOOGLE_CLOUD_PROJECT"), "us-west2")
	return &cloudTaskQueue{
		client:      client,
		queuePrefix: prefix,
	}, nil
}

func (q *cloudTaskQueue) SubmitTask(ctx context.Context, task *Task) error {
	_, err := q.client.CreateTask(ctx, &cloudtaskspb.CreateTaskRequest{
		Parent: q.queuePrefix + task.queueName,
		Task: &cloudtaskspb.Task{
			MessageType: &cloudtaskspb.Task_AppEngineHttpRequest{
				AppEngineHttpRequest: &cloudtaskspb.AppEngineHttpRequest{
					HttpMethod:  task.method,
					RelativeUri: task.url,
					Body:        []byte(task.values.Encode()),
				},
			},
			ScheduleTime: nil,
		},
	})
	return err
}

func (q *cloudTaskQueue) Close() error {
	return q.client.Close()
}

func SubmitTask(ctx context.Context, url string, values url.Values, queueName string) error {
	queue, err := NewCloudTaskQueue(ctx)
	if err != nil {
		return err
	}
	defer queue.Close()
	return queue.SubmitTask(ctx, NewPostTask(url, values, queueName))
}
