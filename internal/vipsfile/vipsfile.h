#include <vips/vips.h>

int ib_load_image_from_file(const char *filename, VipsImage **out);
int ib_thumbnail_from_file(const char *filename, int width, int height, int crop, int size, VipsImage **out);
int ib_save_webp_file(VipsImage *in, const char *filename, int strip, int quality, int lossless, int near_lossless, int reduction_effort, const char *icc_profile, int min_size, int kmin, int kmax);
void ib_unref_image(VipsImage *in);
void ib_get_image_info(VipsImage *in, int *width, int *height, int *has_alpha);
