# go read

A google reader clone built with go on app engine and angularjs.
This was copied from mjibson/goread and modified to work with go116 env and moved to go modules.

# Current build instructions:
  Building: `GOPATH=$(GOPATH) go build`
  Testing:
  ```
    # Start firestore emulator in datastore mode, killing and starting creates the data again.
    JAVA_HOME=$(/usr/libexec/java_home -v 21) gcloud emulators firestore start --database-mode=datastore-mode --host-port=[::1]:9090

    # Build goapp and run it.
    go build && DATASTORE_EMULATOR_HOST=[::1]:9090 ./goread

    # Optionally truncate task.txt, goread will append to it automatically.
    truncate -s 0 task.txt

    # After task.txt is created you run this, it will check in loop
    go run cmd/tasksubmit/tasksubmit.go task.txt

    # To process the feeds, run this. 
    curl -H 'X-Appengine-Taskname: test' http://localhost:8080/tasks/update-feeds

    # Download from https://github.com/remko/dsadmin, you can run queries from the webpage to the datastore
    DATASTORE_EMULATOR_HOST=[::1]:9090 ./dsadmin --project dummy-emulator-datastore-project --port 9091
  ```

  Deploy: `GOPATH=$(GOPATH) gcloud beta app deploy`
  First deploy:
    gcloud config set project $(projectId)
    gcloud app create
    gcloud services enable cloudbuild.googleapis.com cloudresourcemanager.googleapis.com artifactregistry.googleapis.com cloudtasks.googleapis.com
    gcloud beta app deploy
    gcloud beta app deploy index.yaml
    gcloud app deploy queue.yaml cron.yaml
