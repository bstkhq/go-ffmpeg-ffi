package @@APP_ID@@;

import android.content.Context;
import android.util.Log;

import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;

import @@JAVA_PKG@@.@@GO_PKG@@.Mobile;

final class MediaFixture {
  private static final String TAG = "@@LOG_TAG@@";

  private MediaFixture() {}

  static void prepare(Context context) {
    File media = new File(context.getCacheDir(), "ffmpeg-test.mp4");
    File staged = null;
    try {
      staged = File.createTempFile("ffmpeg-test-", ".mp4", context.getCacheDir());
      try (InputStream input = context.getAssets().open("test.mp4");
           OutputStream output = new FileOutputStream(staged, false)) {
        byte[] buffer = new byte[8192];
        int read;
        while ((read = input.read(buffer)) != -1) {
          output.write(buffer, 0, read);
        }
      }
      Files.move(
          staged.toPath(),
          media.toPath(),
          StandardCopyOption.ATOMIC_MOVE,
          StandardCopyOption.REPLACE_EXISTING);
    } catch (Exception e) {
      throw new IllegalStateException("cannot prepare audiovisual fixture", e);
    } finally {
      if (staged != null && staged.exists() && !staged.delete()) {
        Log.w(TAG, "cannot remove staged media fixture: " + staged.getAbsolutePath());
      }
    }
    Mobile.setMediaPath(media.getAbsolutePath());
    Log.i(TAG, "media fixture ready: " + media.getAbsolutePath());
  }
}
