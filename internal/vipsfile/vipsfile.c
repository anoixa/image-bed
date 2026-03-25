#include "vipsfile.h"

int ib_load_image_from_file(const char *filename, VipsImage **out) {
  *out = vips_image_new_from_file(filename, NULL);
  return *out == NULL;
}

int ib_thumbnail_from_file(const char *filename, int width, int height, int crop, int size, VipsImage **out) {
  if (height <= 0) {
    return vips_thumbnail(filename, out, width,
                          "crop", crop,
                          "size", size,
                          NULL);
  }

  return vips_thumbnail(filename, out, width,
                        "height", height,
                        "crop", crop,
                        "size", size,
                        NULL);
}

int ib_save_webp_file(VipsImage *in, const char *filename, int strip, int quality, int lossless, int near_lossless, int reduction_effort, const char *icc_profile, int min_size, int kmin, int kmax) {
  return vips_webpsave(in, filename,
                       "strip", strip,
                       "Q", quality,
                       "lossless", lossless,
                       "near_lossless", near_lossless,
                       "reduction_effort", reduction_effort,
                       "profile", icc_profile,
                       "min_size", min_size,
                       "kmin", kmin,
                       "kmax", kmax,
                       NULL);
}

void ib_unref_image(VipsImage *in) {
  if (in != NULL) {
    g_object_unref(in);
  }
}

void ib_get_image_info(VipsImage *in, int *width, int *height, int *has_alpha) {
  if (width != NULL) {
    *width = vips_image_get_width(in);
  }
  if (height != NULL) {
    *height = vips_image_get_height(in);
  }
  if (has_alpha != NULL) {
    *has_alpha = vips_image_hasalpha(in);
  }
}
