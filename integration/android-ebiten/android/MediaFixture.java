package @@APP_ID@@;

import android.content.Context;
import android.util.Log;

import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;

import @@JAVA_PKG@@.@@GO_PKG@@.Mobile;

final class MediaFixture {
  private static final String TAG = "@@LOG_TAG@@";

  private MediaFixture() {}

  static void prepare(Context context) {
    File media = new File(context.getCacheDir(), "ffgo-test.mp4");
    try (InputStream input = context.getAssets().open("test.mp4");
         OutputStream output = new FileOutputStream(media, false)) {
      byte[] buffer = new byte[8192];
      int read;
      while ((read = input.read(buffer)) != -1) {
        output.write(buffer, 0, read);
      }
    } catch (Exception e) {
      throw new IllegalStateException("cannot prepare audiovisual fixture", e);
    }
    Mobile.setMediaPath(media.getAbsolutePath());
    Log.i(TAG, "media fixture ready: " + media.getAbsolutePath());
  }
}
