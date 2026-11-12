FROM golang:1.27.4 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY main.go issue.go ./

# Compile
RUN CGO_ENABLED=0 go build -trimpath -o /out/license-server .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/license-server /license-server

EXPOSE 8443

ENTRYPOINT ["/license-server"]
