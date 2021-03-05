# go-kit add GRPC via Google Cloud Run

This sample presents:
- `cmd/add`: go-kit base GRPC application that provide two method `Sum` and `Concat`
- `cmd/addcli`: a small go cli to send message to GRPC server

## The Protocol Buffer Definition

this is go-kit base GRPC application that support two method `Sum` and `Concat`, Take a look in [pb/add/add.proto](pb/add/add.proto)

```protobuf
service Add {
  rpc Sum(SumRequest) returns (SumResponse);
  rpc Concat(ConcatRequest) returns (ConcatResponse);
}

message SumRequest {
  int64 a = 1;
  int64 b = 2;
}

message SumResponse {
  int64 res = 1;
  string err = 2;
}

message ConcatRequest {
  string a = 1;
  string b = 2;
}

message ConcatResponse {
  string res = 1;
  string err = 2;
}
```

## The Server

go-kit base `Add` service project layout (ref [如何透過 Go-kit 快速搭建微服務架構應用程式實戰](https://www.slideshare.net/cagechung/gokit-239269720))

```
├── cmd
│   ├── add
│   │   └── main.go
│   └── addcli
│       └── main.go
├── internal
│   ├── app
│   │   └── add
│   │       ├── endpoints
│   │       │   ├── endpoints.go
│   │       │   ├── middleware.go
│   │       │   ├── requests.go
│   │       │   └── responses.go
│   │       ├── service
│   │       │   ├── logging.go
│   │       │   ├── service.go
│   │       │   └── version.go
│   │       └── transports
│   │           └── grpc
│   └── pkg
│       ├── errors
│       │   └── errors.go
│       └── responses
│           ├── decode.go
│           ├── errors.go
│           ├── httpstatus.go
│           └── responses.go
├── pb
│   └── add
│       ├── add.pb.go
│       ├── add.proto
│       └── compile.sh
```

## Running Locally

Now let's start add service locally

```sh
go run cmd/add/main.go
```

There are tow way you connect add GRPC service

use cmd/addcli 
```sh
go run cmd/addcli/main.go -server=localhost:8181 -insecure=true -method=sum 1 1
go run cmd/addcli/main.go -server=localhost:8181 -insecure=true -method=concat 1 1
```

user grpcurl
```sh
grpcurl -plaintext -proto ./pb/add/add.proto -d '{"a": 1, "b":1}' localhost:8181 pb.Add.Sum
```

## PreBuild

Setup GCP project

```sh
gcloud config set project <project-name>
GCP_PROJECT=$(gcloud config get-value project)
GCP_PROJECT_NUMBER=$(gcloud projects list --filter="$GCP_PROJECT" --format="value(PROJECT_NUMBER)")
```

## Build Docker Image

build docker image via build pack locally

```sh
DOCKER_IMAGE=gcr.io/${GCP_PROJECT}/gokit-add-cloud-run
skaffold build
docker push ${DOCKER_IMAGE}
```

or use gcloud builds and push docker image to `gcr.io`

```sh
DOCKER_IMAGE=gcr.io/${GCP_PROJECT}/gokit-add-cloud-run
gcloud builds submit --pack builder=gcr.io/buildpacks/builder:v1,env=GOOGLE_BUILDABLE=cmd/add/main.go,image=${DOCKER_IMAGE}
```

## Deploy

1. Deploy cloud run via gcloud command

    ```sh
    GCP_REGION=asia-east1
    CLOUD_RUN_NAME=gokit-add-cloud-run
    gcloud run deploy ${CLOUD_RUN_NAME} \
        --image=${DOCKER_IMAGE} \
        --platform=managed \
        --allow-unauthenticated \
        --project=${GCP_PROJECT} \
        --region=${GCP_REGION}
    ```

1. Connecting

    ```sh
    ENDPOINT=$(\
    gcloud run services list \
    --project=${GCP_PROJECT} \
    --region=${GCP_REGION} \
    --platform=managed \
    --format="value(status.address.url)" \
    --filter="metadata.name=${CLOUD_RUN_NAME}") 
    ENDPOINT=${ENDPOINT#https://} && echo ${ENDPOINT}
    ```

1. We'll account for that in our `grpcurl` invocation by omitting the `-plaintext` flag:

    ```sh
    grpcurl -proto ./pb/add/add.proto -d '{"a":1, "b":1}' ${ENDPOINT}:443 pb.Add.Sum
    grpcurl -proto ./pb/add/add.proto -d '{"a":"1", "b":"1"}' ${ENDPOINT}:443 pb.Add.Concat
    ```

    or use `cmd/addcli`

    ```sh
    go run cmd/addcli/main.go -server=${ENDPOINT}:443 -insecure=false -method=sum 1 1
    go run cmd/addcli/main.go -server=${ENDPOINT}:443 -insecure=false -method=concat 1 1
    ```

### Clearing

```
gcloud run services delete ${CLOUD_RUN_NAME} --region=${GCP_REGION} --platform=managed
```    