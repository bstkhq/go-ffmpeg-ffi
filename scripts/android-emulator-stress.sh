#!/usr/bin/env bash

set -euo pipefail

readonly package_name="${ANDROID_PACKAGE:-dev.bstk.ffmpeg.probe}"
readonly activity_name="${ANDROID_ACTIVITY:-dev.bstk.ffmpeg.probe/.MainActivity}"
readonly full_cycles="${FFMPEG_ANDROID_FULL_CYCLES:-20}"
readonly rapid_cycles="${FFMPEG_ANDROID_RAPID_CYCLES:-30}"
readonly rapid_active_seconds="${FFMPEG_ANDROID_RAPID_ACTIVE_SECONDS:-1}"
readonly full_timeout_seconds="${FFMPEG_ANDROID_FULL_TIMEOUT_SECONDS:-180}"
readonly lifecycle_timeout_seconds="${FFMPEG_ANDROID_LIFECYCLE_TIMEOUT_SECONDS:-60}"
readonly max_pss_growth_kb="${FFMPEG_ANDROID_MAX_PSS_GROWTH_KB:-65536}"
readonly max_rss_growth_kb="${FFMPEG_ANDROID_MAX_RSS_GROWTH_KB:-65536}"
readonly max_fd_growth="${FFMPEG_ANDROID_MAX_FD_GROWTH:-8}"
readonly max_thread_growth="${FFMPEG_ANDROID_MAX_THREAD_GROWTH:-8}"

if [[ -n "${ADB_BIN:-}" ]]; then
	readonly adb_bin="$ADB_BIN"
elif [[ -n "${ANDROID_SDK_ROOT:-}" ]]; then
	readonly adb_bin="$ANDROID_SDK_ROOT/platform-tools/adb"
else
	readonly adb_bin="$(command -v adb || true)"
fi

if [[ -z "$adb_bin" || ! -x "$adb_bin" ]]; then
	echo "adb was not found; set ADB_BIN or ANDROID_SDK_ROOT" >&2
	exit 1
fi

for value_name in \
	full_cycles rapid_cycles rapid_active_seconds full_timeout_seconds \
	lifecycle_timeout_seconds max_pss_growth_kb max_rss_growth_kb \
	max_fd_growth max_thread_growth; do
	value="${!value_name}"
	if [[ ! "$value" =~ ^[0-9]+$ ]]; then
		echo "$value_name must be a non-negative integer, got $value" >&2
		exit 1
	fi
done

readonly repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly artifact_root="${FFMPEG_ANDROID_ARTIFACT_DIR:-$repo_root/.build/android-artifacts}"
readonly run_id="$(date -u +%Y%m%dT%H%M%SZ)"
readonly metrics_file="$artifact_root/android-stress-$run_id.csv"
readonly logcat_file="$artifact_root/android-stress-$run_id.logcat.txt"
readonly summary_file="$artifact_root/android-stress-$run_id.summary.txt"
mkdir -p "$artifact_root"

tagged_logcat() {
	"$adb_bin" logcat -d -v threadtime GoLog:I AndroidRuntime:E libc:F ActivityManager:E '*:S'
}

# Preserve diagnostics even when a timeout, process restart, or threshold makes
# the runner exit before the normal success epilogue.
trap 'tagged_logcat >"$logcat_file" 2>/dev/null || true' EXIT

count_log_lines() {
	local needle="$1"
	tagged_logcat | awk -v needle="$needle" 'index($0, needle) { count++ } END { print count + 0 }'
}

wait_for_count() {
	local needle="$1"
	local expected="$2"
	local timeout_seconds="$3"
	local deadline=$((SECONDS + timeout_seconds))
	local observed

	while (( SECONDS <= deadline )); do
		observed="$(count_log_lines "$needle")"
		if (( observed >= expected )); then
			return 0
		fi
		sleep 2
	done
	echo "timeout waiting for '$needle': observed=$observed expected=$expected" >&2
	return 1
}

current_pid() {
	"$adb_bin" shell pidof "$package_name" | tr -d '\r'
}

sample_metrics() {
	local phase="$1"
	local cycle="$2"
	local attempt pid memory_rollup status pss_kb rss_kb vmrss_kb threads fds

	for attempt in {1..6}; do
		pid="$(current_pid)"
		if [[ -z "$pid" ]]; then
			echo "the Android probe process is not running" >&2
			return 1
		fi
		if [[ -n "${initial_pid:-}" && "$pid" != "$initial_pid" ]]; then
			echo "the Android probe PID changed from $initial_pid to $pid" >&2
			return 1
		fi

		memory_rollup="$(timeout 30 "$adb_bin" shell \
			"run-as $package_name cat /proc/$pid/smaps_rollup" 2>/dev/null || true)"
		status="$(timeout 30 "$adb_bin" shell \
			"run-as $package_name cat /proc/$pid/status" 2>/dev/null || true)"
		pss_kb="$(printf '%s\n' "$memory_rollup" | awk '/^Pss:/ { print $2; exit }')"
		rss_kb="$(printf '%s\n' "$memory_rollup" | awk '/^Rss:/ { print $2; exit }')"
		vmrss_kb="$(printf '%s\n' "$status" | awk '/^VmRSS:/ { print $2; exit }')"
		threads="$(printf '%s\n' "$status" | awk '/^Threads:/ { print $2; exit }')"
		fds="$(timeout 30 "$adb_bin" shell \
			"run-as $package_name sh -c 'find /proc/$pid/fd -maxdepth 1 -type l | wc -l'" \
			2>/dev/null | tr -d '\r ' || true)"

		if [[ "$pss_kb" =~ ^[0-9]+$ && "$rss_kb" =~ ^[0-9]+$ &&
			"$vmrss_kb" =~ ^[0-9]+$ && "$threads" =~ ^[0-9]+$ && "$fds" =~ ^[0-9]+$ ]]; then
			break
		fi
		echo "metric sample $phase/$cycle incomplete on attempt $attempt; retrying" >&2
		sleep 3
	done
	if [[ ! "$pss_kb" =~ ^[0-9]+$ || ! "$rss_kb" =~ ^[0-9]+$ ||
		! "$vmrss_kb" =~ ^[0-9]+$ || ! "$threads" =~ ^[0-9]+$ || ! "$fds" =~ ^[0-9]+$ ]]; then
		echo "could not parse process metrics for PID $pid after 6 attempts" >&2
		return 1
	fi
	printf '%s,%s,%s,%s,%s,%s,%s,%s,%s\n' \
		"$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$phase" "$cycle" "$pid" \
		"$pss_kb" "$rss_kb" "$vmrss_kb" "$threads" "$fds" | tee -a "$metrics_file"
}

background_and_wait() {
	local shutdown_before
	shutdown_before="$(count_log_lines 'Android lifecycle shutdown: OK')"
	"$adb_bin" shell am start \
		-a android.intent.action.MAIN \
		-c android.intent.category.HOME >/dev/null 2>&1
	wait_for_count 'Android lifecycle shutdown: OK' "$((shutdown_before + 1))" "$lifecycle_timeout_seconds"
}

foreground_and_wait_for_success() {
	local success_before
	success_before="$(count_log_lines 'FFmpeg/H.264/AAC: OK')"
	"$adb_bin" shell am start -n "$activity_name" >/dev/null 2>&1
	wait_for_count 'FFmpeg/H.264/AAC: OK' "$((success_before + 1))" "$full_timeout_seconds"
}

check_failures() {
	local failures
	failures="$(tagged_logcat | awk -v pid="$initial_pid" '
		/Android lifecycle shutdown: FAILED/ ||
		/FFmpeg load: FAILED/ ||
		/decode\/resample\/playback: FAILED/ ||
		/H\.264 decode\/scale: FAILED/ ||
		(/FATAL EXCEPTION/ && $3 == pid) ||
		(/Fatal signal/ && $3 == pid) ||
		/ANR in dev\.bstk\.ffmpeg\.probe/ { print }
	')"
	if [[ -n "$failures" ]]; then
		echo "Android failures found in logcat:" >&2
		printf '%s\n' "$failures" >&2
		return 1
	fi
}

if [[ "$("$adb_bin" get-state 2>/dev/null)" != device ]]; then
	echo "no ready Android device is connected" >&2
	exit 1
fi
if [[ -z "$(current_pid)" ]]; then
	echo "launch $activity_name and wait for its first successful probe before running stress" >&2
	exit 1
fi

readonly initial_pid="$(current_pid)"
printf '%s\n' 'timestamp,phase,cycle,pid,pss_kb,rss_kb,vmrss_kb,threads,fds' >"$metrics_file"
sample_metrics baseline 0

for ((cycle = 1; cycle <= full_cycles; cycle++)); do
	background_and_wait
	foreground_and_wait_for_success
	sample_metrics full "$cycle"
	echo "full lifecycle cycle $cycle/$full_cycles passed"
done

if (( rapid_cycles > 0 )); then
	# Full cycles end in the foreground. Move to a known background state once;
	# each rapid cycle then starts, resumes, cancels, and ends in that same state.
	background_and_wait
fi
for ((cycle = 1; cycle <= rapid_cycles; cycle++)); do
	resume_before="$(count_log_lines 'onResume: resumeGame ok')"
	"$adb_bin" shell am start -n "$activity_name" >/dev/null 2>&1
	wait_for_count 'onResume: resumeGame ok' "$((resume_before + 1))" "$lifecycle_timeout_seconds"
	sleep "$rapid_active_seconds"
	background_and_wait
	if (( cycle % 10 == 0 || cycle == rapid_cycles )); then
		sample_metrics rapid "$cycle"
	fi
	echo "rapid cancellation cycle $cycle/$rapid_cycles passed"
done

foreground_and_wait_for_success
sample_metrics recovery 1
check_failures
tagged_logcat >"$logcat_file"

awk -F, \
	-v max_pss_growth="$max_pss_growth_kb" \
	-v max_rss_growth="$max_rss_growth_kb" \
	-v max_fd_growth="$max_fd_growth" \
	-v max_thread_growth="$max_thread_growth" '
	NR == 2 {
		baseline_pss = $5; baseline_rss = $6; baseline_threads = $8; baseline_fds = $9
		max_pss = $5; max_rss = $6; max_threads = $8; max_fds = $9
	}
	NR > 1 {
		final_pss = $5; final_rss = $6; final_threads = $8; final_fds = $9
		if ($5 > max_pss) max_pss = $5
		if ($6 > max_rss) max_rss = $6
		if ($8 > max_threads) max_threads = $8
		if ($9 > max_fds) max_fds = $9
	}
	END {
		pss_growth = final_pss - baseline_pss
		rss_growth = final_rss - baseline_rss
		thread_growth = final_threads - baseline_threads
		fd_growth = final_fds - baseline_fds
		printf "baseline_pss_kb=%d final_pss_kb=%d max_pss_kb=%d growth_pss_kb=%d\n", baseline_pss, final_pss, max_pss, pss_growth
		printf "baseline_rss_kb=%d final_rss_kb=%d max_rss_kb=%d growth_rss_kb=%d\n", baseline_rss, final_rss, max_rss, rss_growth
		printf "baseline_threads=%d final_threads=%d max_threads=%d growth_threads=%d\n", baseline_threads, final_threads, max_threads, thread_growth
		printf "baseline_fds=%d final_fds=%d max_fds=%d growth_fds=%d\n", baseline_fds, final_fds, max_fds, fd_growth
		if (pss_growth > max_pss_growth || rss_growth > max_rss_growth ||
			thread_growth > max_thread_growth || fd_growth > max_fd_growth) exit 1
	}
' "$metrics_file" | tee "$summary_file"

echo "Android prolonged/stress test passed"
echo "metrics: $metrics_file"
echo "logcat: $logcat_file"
echo "summary: $summary_file"
