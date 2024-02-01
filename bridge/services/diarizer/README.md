# diarizer
Memory usage processing the audio clip peaked around 1.7G. Image size is 9G

### Build container
```
docker build -t diarizer .
```
### Run container
```
docker run --rm -it -p 8000:8000 diarizer
```
### Run test
```
go run test.go
```
