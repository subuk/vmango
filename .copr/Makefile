# make -f .copr/Makefile srpm outdir="/tmp/coprtest" spec="<spec_path>"
outdir =
spec =

srpm:
	yum install -y git || dnf install -y git
	$(MAKE) srpm RPM_OUTDIR=$(outdir) RPM_SPEC=$(spec)
