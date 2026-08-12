# This fixture is a downstream module: Ebitengine is never added to the
# go-ffmpeg-ffi module graph.
APP_NAME ?= go-ffmpeg-ffi Android probe
APP_ID ?= dev.bstk.ffgo.probe
GO_SRC ?= $(abspath .)
ANDROID_TARGET ?= android/arm64
ANDROID_MIN_SDK ?= 33

# By default keep the reusable builder and all Gradle artifacts inside this
# fixture's ignored .build directory. Override BUILDER_DIR to reuse a checkout.
BUILDER_DIR ?= .build/apk-ebiten-builder
BUILDER_REPO ?= https://github.com/bstkhq/apk-ebiten-builder
ROOT_DIR ?= $(abspath $(BUILDER_DIR))
INCLUDE_PATH ?= $(BUILDER_DIR)/Include.mk

export APP_ID

$(INCLUDE_PATH):
	git clone $(BUILDER_REPO) $(BUILDER_DIR)

include $(INCLUDE_PATH)

# FFmpeg is packaged by the downstream application, never by the binding.
FFMPEG_ANDROID_ROOT ?= $(abspath ../../.build/ffmpeg-android/install)

ifeq ($(ANDROID_TARGET),android/arm64)
  ANDROID_ABI := arm64-v8a
else ifeq ($(ANDROID_TARGET),android/amd64)
  ANDROID_ABI := x86_64
else
  $(error ANDROID_TARGET must be android/arm64 or android/amd64)
endif

FFMPEG_ANDROID_LIB_DIR ?= $(FFMPEG_ANDROID_ROOT)/$(ANDROID_ABI)/lib
JNI_LIB_DIR := $(ANDROID_DIR)/app/src/main/jniLibs/$(ANDROID_ABI)

configure_android_floor: generate
	$(call LOG,Setting Android minimum SDK to API $(ANDROID_MIN_SDK))
	$(Q)perl -pi -e 's/^([[:space:]]*)minSdk[[:space:]]+[0-9]+/$$1minSdk $(ANDROID_MIN_SDK)/' \
		"$(ANDROID_DIR)/app/build.gradle"

package_ffmpeg: generate
	$(call LOG,Packaging FFmpeg shared libraries for $(ANDROID_ABI))
	$(Q)test -f "$(FFMPEG_ANDROID_LIB_DIR)/libavcodec.so" || { \
		echo "FFmpeg Android libraries not found in $(FFMPEG_ANDROID_LIB_DIR)"; \
		echo "Run ../../scripts/build-ffmpeg-android.sh $(patsubst android/%,%,$(ANDROID_TARGET)) first."; \
		exit 1; \
	}
	$(Q)mkdir -p "$(JNI_LIB_DIR)"
	$(Q)install -m 755 "$(FFMPEG_ANDROID_LIB_DIR)"/*.so "$(JNI_LIB_DIR)/"

apk_with_ffmpeg: configure_android_floor compile package_ffmpeg
	$(call LOG,Building debug APK with FFmpeg for $(ANDROID_ABI))
	$(call GRADLE_RUN,assembleDebug)
	@echo -n "    Output File: "
	@realpath -m --relative-to="$(ROOT_DIR)" "$(APK_DEBUG)"

.PHONY: configure_android_floor package_ffmpeg apk_with_ffmpeg
