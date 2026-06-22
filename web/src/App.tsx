import { Routes, Route, Navigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError, type Me } from "./lib/api";
import { LoadingState } from "./components/ui";
import { Layout } from "./components/Layout";
import { Login } from "./pages/Login";
import { Services } from "./pages/Services";
import { ServiceDetail } from "./pages/ServiceDetail";
import { Audit } from "./pages/Audit";
import { Admin } from "./pages/Admin";

export default function App() {
  const me = useQuery<Me, ApiError>({ queryKey: ["me"], queryFn: api.me });

  if (me.isLoading) {
    return <LoadingState label="Starting…" />;
  }

  if (me.error?.status === 401 || !me.data) {
    return <Login />;
  }

  return (
    <Layout me={me.data}>
      <Routes>
        <Route path="/" element={<Services me={me.data} />} />
        <Route path="/services/:id" element={<ServiceDetail me={me.data} />} />
        <Route path="/audit" element={<Audit />} />
        <Route path="/admin" element={<Admin me={me.data} />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  );
}
