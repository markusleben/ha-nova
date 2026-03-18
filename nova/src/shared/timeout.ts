export class TimeoutError extends Error {}

export function withTimeout<T>(promise: Promise<T>, timeoutMs: number, onTimeout?: () => void): Promise<T> {
  let timeoutHandle: NodeJS.Timeout | undefined;

  return new Promise<T>((resolve, reject) => {
    timeoutHandle = setTimeout(() => {
      onTimeout?.();
      reject(new TimeoutError("Request timed out"));
    }, timeoutMs);

    promise
      .then((value) => {
        resolve(value);
      })
      .catch((error: unknown) => {
        reject(error);
      })
      .finally(() => {
        if (timeoutHandle) {
          clearTimeout(timeoutHandle);
        }
      });
  });
}
