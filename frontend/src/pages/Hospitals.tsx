// Everyone: the hospitals in the chain.
import { api } from '../api';
import { Card, Status, useLoad } from '../components/common';

export default function Hospitals() {
  const { data, error, loading } = useLoad(() => api.hospitals(), []);
  const items = [...(data?.items ?? [])].sort((a, b) => a.hospitalId.localeCompare(b.hospitalId));
  return (
    <Card title="Hospitals in the chain">
      <Status loading={loading} error={error} />
      {data && (
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>City</th>
              <th>Phone</th>
            </tr>
          </thead>
          <tbody>
            {items.map((h) => (
              <tr key={h.hospitalId}>
                <td>{h.hospitalId}</td>
                <td>{h.name}</td>
                <td>{h.city}</td>
                <td>{h.phone}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}
