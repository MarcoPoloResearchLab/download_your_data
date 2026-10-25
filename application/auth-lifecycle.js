// @ts-check

(function installAuthenticationLifecycleBuffer() {
  const pendingStatuses = [];
  let taken = false;

  const captureAuthenticated = () => pendingStatuses.push('authenticated');
  const captureUnauthenticated = () => pendingStatuses.push('unauthenticated');

  document.addEventListener(
    'mpr-ui:auth:authenticated',
    captureAuthenticated
  );
  document.addEventListener(
    'mpr-ui:auth:unauthenticated',
    captureUnauthenticated
  );

  const lifecycleBuffer = Object.freeze({
    take() {
      if (taken) {
        throw new Error('authentication lifecycle buffer was already consumed');
      }
      taken = true;
      document.removeEventListener(
        'mpr-ui:auth:authenticated',
        captureAuthenticated
      );
      document.removeEventListener(
        'mpr-ui:auth:unauthenticated',
        captureUnauthenticated
      );
      return pendingStatuses.splice(0);
    }
  });

  Object.defineProperty(window, 'DownloadYourDataAuthLifecycle', {
    configurable: false,
    enumerable: false,
    writable: false,
    value: lifecycleBuffer
  });
})();
