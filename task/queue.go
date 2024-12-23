package task

import (
	"context"
	"fmt"
	"net/url"
	"os"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"github.com/harishjp/goread/config"
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
	if config.IsDevServer() {
		file, err := os.OpenFile("task.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		return &fileTaskQueue{
			file: file,
		}, nil
	}
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	prefix := fmt.Sprintf("projects/%s/locations/%s/queues/", config.ProjectID(), config.Region())
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

type fileTaskQueue struct {
	file *os.File
}

func (q *fileTaskQueue) SubmitTask(ctx context.Context, task *Task) error {
	data := task.values.Encode() + " " + config.RootURL() + task.url + "\n"
	_, err := q.file.Write([]byte(data))
	return err
}

func (q *fileTaskQueue) Close() error {
	return q.file.Close()
}

func SubmitTask(ctx context.Context, url string, values url.Values, queueName string) error {
	queue, err := NewCloudTaskQueue(ctx)
	if err != nil {
		return err
	}
	defer queue.Close()
	return queue.SubmitTask(ctx, NewPostTask(url, values, queueName))
}
