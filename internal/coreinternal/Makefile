include ../../Makefile.Common

misspell: misspell-skip-files

LOCALIZED_TABLES_FILE := /timeutils/internal/localizedtime/tables.go

# remove the LOCALIZED_TABLES_FILE from the Makefile.Common's MISSPELL_CMD variable
MISSPELL_CMD := $(patsubst %$(LOCALIZED_TABLES_FILE),,$(MISSPELL_CMD))

misspell-skip-files:
	@echo "misspell skipped for file: $(LOCALIZED_TABLES_FILE)"