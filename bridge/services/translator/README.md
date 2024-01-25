### Build container
Requires Docker VM to have 16GB+ RAM
```
docker build -t translator .
```
### Run container
```
docker run --rm -it -p 8000:8000 translator
```
### Run test
```
go run test.go
```