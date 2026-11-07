package server

import "net/http"

func (s *Server) handleCreateArtifact(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleCreateArtifact(w, r)
}
func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListArtifacts(w, r)
}
func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleGetArtifact(w, r, id)
}
func (s *Server) handlePublishArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandlePublish(w, r, id)
}
func (s *Server) handleUnpublishArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleUnpublish(w, r, id)
}
func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleDeleteArtifact(w, r, id)
}
func (s *Server) handleMintArtifactToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleMintToken(w, r, id)
}
func (s *Server) handleRevokeArtifactToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tid := r.PathValue("tid")
	s.handler.HandleRevokeToken(w, r, id, tid)
}
func (s *Server) handleAllowlistAddArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleAllowlistAdd(w, r, id)
}
func (s *Server) handleAllowlistRemoveArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	email := r.PathValue("email")
	s.handler.HandleAllowlistRemove(w, r, id, email)
}
func (s *Server) handleAccessLogArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.handler.HandleAccessLog(w, r, id)
}
