const moduleURL = new URLSearchParams(window.location.search).get("module");

try {
  if (moduleURL === null) {
    throw new Error("the module query parameter is required");
  }
  const { instantiate } = await import(moduleURL);
  const decoder = new TextDecoder();
  let stdout = "";
  let stderr = "";
  const program = await instantiate({
    write(stream, bytes) {
      const text = decoder.decode(bytes, { stream: true });
      if (stream === 2) {
        stderr += text;
      } else {
        stdout += text;
      }
      return true;
    },
  });
  const status = program.start();
  document.querySelector("#status").textContent = String(status);
  document.querySelector("#stdout").textContent = stdout;
  document.querySelector("#stderr").textContent = stderr;
  window.kizuESM = { ready: true, status, stdout, stderr };
} catch (error) {
  const stderr = String(error.stack ?? error);
  document.querySelector("#status").textContent = "error";
  document.querySelector("#stderr").textContent = stderr;
  window.kizuESM = { ready: true, error: stderr };
}
