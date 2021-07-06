docker build -t docker.pkg.github.com/lepers/glavar/glavar .
docker push docker.pkg.github.com/lepers/glavar/glavar
docker build -t badtrousers/glavar .
docker push badtrousers/glavar
