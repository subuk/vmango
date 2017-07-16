DEPLOY_HOST=no_host_set
DEPLOY_PATH=/srv/www/vmango.org/docs/
HUGO = hugo-0.21

dev:
	$(HUGO) server -D -E

deploy: clean
	$(HUGO)
	rsync -v --delete-after -a public/ $(DEPLOY_HOST):$(DEPLOY_PATH)

clean:
	rm -rf public
