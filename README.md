# go read

A google reader clone built with go on app engine and angularjs.
This was copied from mjibson/goread and modified to work with go116 env and moved to go modules.

# Current build instructions:
  Building: `GOPATH=$(GOPATH) go build`
  Deploy: `GOPATH=$(GOPATH) gcloud beta app deploy`
  First deploy:
    gcloud config set project $(projectId)
    gcloud app create
    gcloud services enable cloudbuild.googleapis.com cloudresourcemanager.googleapis.com artifactregistry.googleapis.com cloudtasks.googleapis.com
    gcloud beta app deploy
    gcloud beta app deploy index.yaml
    gcloud app deploy queue.yaml cron.yaml
