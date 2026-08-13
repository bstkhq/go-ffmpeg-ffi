IOS_VERSION ?= 13.0
IOS_TARGET ?= ios
OUTPUT ?= .build/FFGoProbe.xcframework

.PHONY: compile clean

compile:
	mkdir -p "$(dir $(OUTPUT))"
	ebitenmobile bind \
		-target "$(IOS_TARGET)" \
		-iosversion "$(IOS_VERSION)" \
		-o "$(OUTPUT)" .
	test -d "$(OUTPUT)"

clean:
	rm -rf .build
