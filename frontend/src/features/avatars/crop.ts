// A local picture becomes an avatar here, in the browser: cut to the
// largest centered square, scaled to 256×256 and encoded as PNG. The core
// only stores that exact shape.
export const AVATAR_PICTURE_TYPES = ['image/png', 'image/jpeg', 'image/webp'];
export const AVATAR_PICTURE_SIDE = 256;
const MAX_BYTES = 512 * 1024;

export class AvatarPictureError extends Error {
  constructor(readonly messageKey: string) {
    super(messageKey);
  }
}

/** The picture as a base64 PNG and a data URL to preview it. */
export interface CroppedPicture {
  base64: string;
  preview: string;
}

export async function cropToAvatar(file: File): Promise<CroppedPicture> {
  if (!AVATAR_PICTURE_TYPES.includes(file.type)) throw new AvatarPictureError('avatar.pictureType');
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    throw new AvatarPictureError('avatar.pictureUnreadable');
  }
  const side = Math.min(bitmap.width, bitmap.height);
  const canvas = document.createElement('canvas');
  canvas.width = AVATAR_PICTURE_SIDE;
  canvas.height = AVATAR_PICTURE_SIDE;
  const context = canvas.getContext('2d');
  if (!context) throw new AvatarPictureError('avatar.pictureUnreadable');
  context.imageSmoothingQuality = 'high';
  context.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, AVATAR_PICTURE_SIDE, AVATAR_PICTURE_SIDE);
  bitmap.close();
  const preview = canvas.toDataURL('image/png');
  const base64 = preview.slice(preview.indexOf(',') + 1);
  if ((base64.length * 3) / 4 > MAX_BYTES) throw new AvatarPictureError('avatar.pictureTooLarge');
  return { base64, preview };
}
