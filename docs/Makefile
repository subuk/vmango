DEPLOY_HOST=no_host_set
DEPLOY_PATH=/srv/www/vmango.org/docs/

dev:
	hugo server -D -E

deploy: clean
	hugo
	rsync -v --delete-after -a public/ $(DEPLOY_HOST):$(DEPLOY_PATH)

clean:
	rm -rf public
