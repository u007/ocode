export default function RemoteReconnect() {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        height: "100vh",
        gap: "0.75rem",
        fontFamily: "system-ui, sans-serif",
        textAlign: "center",
        padding: "2rem",
      }}
    >
      <h1 style={{ fontSize: "1.25rem", fontWeight: 600 }}>Session expired — reconnect from your terminal</h1>
      <p style={{ color: "#666", maxWidth: "32rem" }}>
        This tab's link to the remote ocode server is no longer valid. Run{" "}
        <code>ocode remote --web &lt;host&gt;</code> again to open a fresh, working tab.
      </p>
    </div>
  );
}
