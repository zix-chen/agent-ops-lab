FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /agent-ops-lab ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /agent-ops-lab /agent-ops-lab
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/agent-ops-lab"]
