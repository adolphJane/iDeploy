FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/ideploy-server ./cmd/ideploy-server

FROM alpine:3.21
RUN addgroup -S ideploy && adduser -S -G ideploy ideploy
COPY --from=build /out/ideploy-server /usr/local/bin/ideploy-server
USER ideploy
EXPOSE 8765
ENTRYPOINT ["/usr/local/bin/ideploy-server"]
