# Multi-stage build: compile static binaries, ship them on a minimal,
# non-root base image. Migrations are embedded in the binaries.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /out/migrate /usr/local/bin/
ENV APP_ENV=production PORT=4000
EXPOSE 4000
# Run migrations as a separate release step: `docker run <image> migrate up`
ENTRYPOINT ["api"]
