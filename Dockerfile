FROM golang:1.27-alpine AS build

ARG SERVICE=api

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /orderflow ./cmd/${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /orderflow /orderflow

EXPOSE 8080

ENTRYPOINT ["/orderflow"]

